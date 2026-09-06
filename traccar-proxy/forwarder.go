package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	minBackoff = 1 * time.Second
	maxBackoff = 5 * time.Minute
)

// Worker drains one target's queue in order, retrying transient failures
// with exponential backoff. Permanent rejections (most 4xx) are dropped so
// one bad request can't block everything behind it.
type Worker struct {
	target  Target
	queue   *Queue
	client  *http.Client
	timeout time.Duration

	mu          sync.Mutex
	delivered   uint64
	rejected    uint64
	attempts    uint64
	lastSuccess time.Time
	lastAttempt time.Time
	lastError   string
	backoff     time.Duration
	nextRetry   time.Time
}

// Snapshot is the read-only view of a worker for the status page.
type Snapshot struct {
	Name        string    `json:"name"`
	Endpoint    string    `json:"endpoint"`
	Queued      int       `json:"queued"`
	Delivered   uint64    `json:"delivered"`
	Rejected    uint64    `json:"rejected"`
	Dropped     uint64    `json:"dropped"`
	LastSuccess time.Time `json:"last_success,omitzero"`
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastError   string    `json:"last_error,omitempty"`
	NextRetry   time.Time `json:"next_retry,omitempty"`
	State       string    `json:"state"` // ok | idle | retrying | failing
}

func newWorker(t Target, q *Queue, timeout time.Duration) *Worker {
	return &Worker{
		target:  t,
		queue:   q,
		timeout: timeout,
		client: &http.Client{
			Timeout: timeout,
			// Never follow redirects: replaying a POST through a 30x would
			// silently change the method or drop the body.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (w *Worker) Run(ctx context.Context) {
	for {
		job, name, _ := w.queue.Peek()
		if job == nil {
			select {
			case <-ctx.Done():
				return
			case <-w.queue.Wake():
			}
			continue
		}

		status, err := w.deliver(ctx, job)
		if ctx.Err() != nil {
			return
		}

		w.mu.Lock()
		w.attempts++
		w.lastAttempt = time.Now()
		switch {
		case err == nil && status/100 == 2:
			w.delivered++
			w.lastSuccess = w.lastAttempt
			w.lastError = ""
			w.backoff = 0
			w.nextRetry = time.Time{}
			w.mu.Unlock()
			w.queue.Ack(name)
			continue

		case err == nil && !retryable(status):
			w.rejected++
			w.lastError = fmt.Sprintf("%d %s", status, http.StatusText(status))
			w.backoff = 0
			w.nextRetry = time.Time{}
			w.mu.Unlock()
			log.Printf("[%s] rejected (%d), dropping %s", w.target.Name, status, name)
			w.queue.Ack(name)
			continue
		}

		if err != nil {
			w.lastError = err.Error()
		} else {
			w.lastError = fmt.Sprintf("%d %s", status, http.StatusText(status))
		}
		if w.backoff == 0 {
			w.backoff = minBackoff
		} else {
			w.backoff = min(w.backoff*2, maxBackoff)
		}
		w.nextRetry = time.Now().Add(w.backoff)
		wait := w.backoff
		w.mu.Unlock()
		log.Printf("[%s] %s; retrying in %s (%d queued)", w.target.Name, w.lastError, wait, w.queue.Len())

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return status >= 500
}

// deliver replays a job against the target. Returns the HTTP status, or an
// error for transport-level failures.
func (w *Worker) deliver(ctx context.Context, j *Job) (int, error) {
	u := buildURL(w.target.URL, j)
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, j.Method, u.String(), bytes.NewReader(j.Body))
	if err != nil {
		return 0, err
	}
	req.ContentLength = int64(len(j.Body))
	if j.ContentType != "" {
		req.Header.Set("Content-Type", j.ContentType)
	}
	req.Header.Set("User-Agent", firstNonEmpty(j.UserAgent, "traccar-proxy"))

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, unwrapURLError(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, nil
}

// buildURL appends the client's path to the target's and forwards the query
// string verbatim, except that any parameter fixed on the target URL replaces
// the client's value (e.g. a per-target device id).
func buildURL(base *url.URL, j *Job) *url.URL {
	u := *base
	if j.Path != "" && j.Path != "/" {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(j.Path, "/")
	}
	u.RawQuery = j.Query
	if base.RawQuery == "" {
		return &u
	}
	fixed := base.Query()
	var kept []string
	for _, pair := range strings.Split(j.Query, "&") {
		if pair == "" {
			continue
		}
		key, _, _ := strings.Cut(pair, "=")
		if k, err := url.QueryUnescape(key); err == nil {
			key = k
		}
		if _, override := fixed[key]; !override {
			kept = append(kept, pair)
		}
	}
	u.RawQuery = strings.Join(append(kept, base.RawQuery), "&")
	return &u
}

func (w *Worker) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := Snapshot{
		Name:        w.target.Name,
		Endpoint:    w.target.Redacted(),
		Queued:      w.queue.Len(),
		Delivered:   w.delivered,
		Rejected:    w.rejected,
		Dropped:     w.queue.dropped.Load(),
		LastSuccess: w.lastSuccess,
		LastAttempt: w.lastAttempt,
		LastError:   w.lastError,
		NextRetry:   w.nextRetry,
	}
	switch {
	case !w.nextRetry.IsZero() && w.backoff >= time.Minute:
		s.State = "failing"
	case !w.nextRetry.IsZero():
		s.State = "retrying"
	case w.attempts == 0:
		s.State = "idle"
	default:
		s.State = "ok"
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// unwrapURLError strips the noisy method/URL prefix Go adds to client errors.
func unwrapURLError(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return ue.Err
	}
	return err
}
