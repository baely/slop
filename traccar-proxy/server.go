package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

//go:embed status.html
var statusFS embed.FS

var statusTmpl = template.Must(template.New("status.html").Funcs(template.FuncMap{
	"ago":   humanize,
	"comma": commas,
}).ParseFS(statusFS, "status.html"))

const maxBody = 1 << 20 // 1 MiB; OsmAnd payloads are a few hundred bytes

type Server struct {
	cfg      Config
	queues   []*Queue
	workers  []*Worker
	started  time.Time
	received atomic.Uint64
}

func newServer(cfg Config, queues []*Queue, workers []*Worker) *Server {
	return &Server{cfg: cfg, queues: queues, workers: workers, started: time.Now()}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "ok\n")
		return
	}

	// Everything else lives under the secret prefix, when one is configured.
	rest, ok := s.stripToken(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case rest == "/api/status" && r.Method == http.MethodGet:
		s.serveStatusJSON(w, r)
	case (rest == "" || rest == "/") && r.Method == http.MethodGet && r.URL.RawQuery == "":
		// A bare GET carries no position, so it can only be a human.
		s.serveStatusPage(w, r)
	default:
		s.ingest(w, r, rest)
	}
}

func (s *Server) stripToken(path string) (string, bool) {
	if s.cfg.Token == "" {
		return path, true
	}
	prefix := "/" + s.cfg.Token
	if path == prefix {
		return "", true
	}
	if strings.HasPrefix(path, prefix+"/") {
		return path[len(prefix):], true
	}
	return "", false
}

func (s *Server) ingest(w http.ResponseWriter, r *http.Request, path string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if len(body) > maxBody {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}

	job := &Job{
		Received:    time.Now(),
		Method:      r.Method,
		Path:        path,
		Query:       r.URL.RawQuery,
		ContentType: r.Header.Get("Content-Type"),
		UserAgent:   r.Header.Get("User-Agent"),
		Body:        body,
	}
	for i, q := range s.queues {
		if err := q.Push(job); err != nil {
			log.Printf("[%s] enqueue failed: %v", s.cfg.Targets[i].Name, err)
			http.Error(w, "enqueue failed", http.StatusInternalServerError)
			return
		}
	}
	s.received.Add(1)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"queued":%d}`+"\n", len(s.queues))
}

type statusView struct {
	ClientURL string     `json:"client_url"`
	Uptime    string     `json:"uptime"`
	Received  uint64     `json:"received"`
	Delivered uint64     `json:"delivered"`
	Queued    int        `json:"queued"`
	Rejected  uint64     `json:"rejected"`
	Targets   []Snapshot `json:"targets"`
	Now       time.Time  `json:"now"`
}

func (s *Server) view(r *http.Request) statusView {
	v := statusView{
		Received: s.received.Load(),
		Uptime:   humanizeDuration(time.Since(s.started)),
		Now:      time.Now(),
	}
	for _, wk := range s.workers {
		snap := wk.Snapshot()
		v.Delivered += snap.Delivered
		v.Queued += snap.Queued
		v.Rejected += snap.Rejected
		v.Targets = append(v.Targets, snap)
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	v.ClientURL = scheme + "://" + r.Host + "/"
	if s.cfg.Token != "" {
		v.ClientURL += s.cfg.Token
	}
	return v
}

func (s *Server) serveStatusJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(s.view(r))
}

func (s *Server) serveStatusPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := statusTmpl.Execute(w, s.view(r)); err != nil {
		log.Printf("status template: %v", err)
	}
}

// humanize renders a past time as "4 minutes ago"; zero times as "never".
func humanize(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < 0 {
		return "in " + humanizeDuration(-d)
	}
	if d < 5*time.Second {
		return "just now"
	}
	return humanizeDuration(d) + " ago"
}

func humanizeDuration(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d < time.Minute:
		return plural(int64(d.Seconds()), "second")
	case d < time.Hour:
		return plural(int64(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return plural(int64(d.Hours()), "hour")
	case d < 14*24*time.Hour:
		return plural(int64(d.Hours()/24), "day")
	default:
		return plural(int64(d.Hours()/24/7), "week")
	}
}

func commas(n any) string {
	var v uint64
	switch x := n.(type) {
	case uint64:
		v = x
	case int:
		v = uint64(x)
	}
	s := fmt.Sprint(v)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
