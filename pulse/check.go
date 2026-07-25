package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// maxBodyRead bounds the keyword scan. A target serving a 2GB file must
	// not be able to exhaust this process.
	maxBodyRead  = 256 << 10
	maxRedirects = 3
	userAgent    = "pulse/1 (+https://pulse.baileys.dev)"
)

// ExpectMatcher matches an HTTP status code against a target's expectation.
type ExpectMatcher struct {
	classes []int // 2 means 2xx
	codes   []int
}

// ParseExpect reads "2xx", "200", "200,204", "2xx,301" and friends.
func ParseExpect(s string) (ExpectMatcher, error) {
	var m ExpectMatcher
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), ",")
	if len(parts) > 8 {
		return m, errors.New("expected status: at most 8 values")
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) == 3 && p[1] == 'x' && p[2] == 'x' && p[0] >= '1' && p[0] <= '5' {
			m.classes = append(m.classes, int(p[0]-'0'))
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 100 || n > 599 {
			return ExpectMatcher{}, fmt.Errorf("expected status: %q is not a code or class like 2xx", p)
		}
		m.codes = append(m.codes, n)
	}
	if len(m.classes) == 0 && len(m.codes) == 0 {
		return m, errors.New("expected status: nothing to match")
	}
	return m, nil
}

// Match reports whether code satisfies the expectation.
func (m ExpectMatcher) Match(code int) bool {
	for _, c := range m.codes {
		if c == code {
			return true
		}
	}
	for _, c := range m.classes {
		if code >= c*100 && code < (c+1)*100 {
			return true
		}
	}
	return false
}

// Checker runs probes on a schedule.
type Checker struct {
	store   *Store
	client  *http.Client
	workers chan struct{}
	logger  *log.Logger
	now     func() time.Time
}

// NewChecker builds a checker with a bounded worker pool.
func NewChecker(s *Store, logger *log.Logger, workers int) *Checker {
	tr := &http.Transport{
		Proxy: nil, // never route estate checks through a proxy
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &Checker{
		store: s,
		client: &http.Client{
			Transport: tr,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("stopped after %d redirects", maxRedirects)
				}
				return nil
			},
		},
		workers: make(chan struct{}, workers),
		logger:  logger,
		now:     time.Now,
	}
}

// Run is the scheduler loop: one goroutine, waking every second, dispatching
// due targets into the worker pool.
func (c *Checker) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			for _, t := range c.store.Due(now) {
				c.dispatch(ctx, t)
			}
		}
	}
}

func (c *Checker) dispatch(ctx context.Context, t Target) {
	select {
	case c.workers <- struct{}{}:
	default:
		c.logger.Printf("check skipped target=%s reason=pool_saturated", t.ID)
		return
	}
	go func() {
		defer func() { <-c.workers }()
		c.RunOne(ctx, t)
	}()
}

// RunOne probes a target and records the result. It never panics out.
func (c *Checker) RunOne(ctx context.Context, t Target) Check {
	chk := c.probeSafe(ctx, t)
	c.store.Record(t.ID, chk)
	return chk
}

func (c *Checker) probeSafe(ctx context.Context, t Target) (chk Check) {
	start := c.now()
	defer func() {
		if r := recover(); r != nil {
			c.logger.Printf("check panic target=%s: %v", t.ID, r)
			chk = Check{
				At:        start,
				OK:        false,
				Err:       "internal check error",
				LatencyMs: int(c.now().Sub(start) / time.Millisecond),
			}
		}
	}()
	return c.Probe(ctx, t)
}

// Probe performs the request and grades it.
func (c *Checker) Probe(ctx context.Context, t Target) Check {
	matcher, err := ParseExpect(t.Expect)
	if err != nil {
		matcher = ExpectMatcher{classes: []int{2}}
	}
	timeout := time.Duration(t.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := c.now()
	chk := Check{At: start}

	method := t.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, t.URL, nil)
	if err != nil {
		chk.Err = "invalid request"
		return chk
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")

	resp, err := c.client.Do(req)
	chk.LatencyMs = int(c.now().Sub(start) / time.Millisecond)
	if err != nil {
		chk.Err = classifyErr(err)
		return chk
	}
	defer resp.Body.Close()
	chk.Status = resp.StatusCode

	var body []byte
	if t.Keyword != "" && method == http.MethodGet {
		body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyRead))
		if err != nil {
			chk.Err = "body read failed: " + classifyErr(err)
			return chk
		}
	}
	// Drain a bounded amount so the connection can be reused, never more.
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyRead))

	if !matcher.Match(resp.StatusCode) {
		chk.Err = fmt.Sprintf("status %d, expected %s", resp.StatusCode, t.Expect)
		return chk
	}
	if t.Keyword != "" && method == http.MethodGet && !bytes.Contains(body, []byte(t.Keyword)) {
		chk.Err = fmt.Sprintf("keyword %q absent from first %d KiB", t.Keyword, maxBodyRead>>10)
		return chk
	}
	chk.OK = true
	return chk
}

// classifyErr turns a transport error into a short, stable, distinct string.
// DNS, TLS, refused connections and timeouts must not read the same.
func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "dns: no such host"
		case dnsErr.IsTimeout:
			return "dns: lookup timed out"
		default:
			return "dns: lookup failed"
		}
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return "tls: certificate not trusted"
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return "tls: certificate name mismatch"
	}
	var authErr x509.UnknownAuthorityError
	if errors.As(err, &authErr) {
		return "tls: unknown certificate authority"
	}
	var recErr tls.RecordHeaderError
	if errors.As(err, &recErr) {
		return "tls: handshake failed, not a tls port"
	}
	var alert *tls.AlertError
	if errors.As(err, &alert) {
		return "tls: handshake rejected"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection refused"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "connection reset"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) {
		return "host unreachable"
	}
	if errors.Is(err, syscall.ENETUNREACH) {
		return "network unreachable"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "check cancelled"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if strings.Contains(urlErr.Err.Error(), "stopped after") {
			return fmt.Sprintf("too many redirects (>%d)", maxRedirects)
		}
		if strings.Contains(urlErr.Err.Error(), "tls:") {
			return "tls: handshake failed"
		}
	}
	if strings.Contains(err.Error(), "no route to host") {
		return "host unreachable"
	}
	return "request failed"
}
