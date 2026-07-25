package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testChecker(t *testing.T, s *Store) *Checker {
	t.Helper()
	return NewChecker(s, log.New(io.Discard, "", 0), 4)
}

// clock is a settable time source so incident durations are exact in tests.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func TestParseExpect(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		match   map[int]bool
	}{
		{in: "2xx", match: map[int]bool{200: true, 204: true, 299: true, 301: false, 500: false}},
		{in: "200", match: map[int]bool{200: true, 201: false}},
		{in: "200,204", match: map[int]bool{200: true, 204: true, 202: false}},
		{in: "2xx,301", match: map[int]bool{200: true, 301: true, 302: false}},
		{in: "3xx", match: map[int]bool{301: true, 200: false}},
		{in: " 4xx ", match: map[int]bool{404: true, 200: false}},
		{in: "okay", wantErr: true},
		{in: "99", wantErr: true},
		{in: "600", wantErr: true},
		{in: "", wantErr: true},
		{in: "6xx", wantErr: true},
		{in: "200,201,202,203,204,205,206,207,208", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			m, err := ParseExpect(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			for code, want := range tc.match {
				if got := m.Match(code); got != want {
					t.Fatalf("%q matching %d = %v, want %v", tc.in, code, got, want)
				}
			}
		})
	}
}

func TestProbeGrading(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "all good, the marker is here")
		case "/500":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/404":
			http.NotFound(w, r)
		case "/big":
			// Far more than the read cap; the keyword sits past the end.
			w.Write([]byte(strings.Repeat("x", maxBodyRead+4096)))
			w.Write([]byte("needle"))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := testChecker(t, s)

	cases := []struct {
		name    string
		target  Target
		wantOK  bool
		status  int
		errPart string
	}{
		{name: "2xx ok", target: Target{URL: srv.URL + "/ok", Method: "GET", Expect: "2xx", Timeout: 5}, wantOK: true, status: 200},
		{name: "keyword present", target: Target{URL: srv.URL + "/ok", Method: "GET", Expect: "2xx", Timeout: 5, Keyword: "marker"}, wantOK: true, status: 200},
		{name: "keyword absent", target: Target{URL: srv.URL + "/ok", Method: "GET", Expect: "2xx", Timeout: 5, Keyword: "absent"}, status: 200, errPart: "absent from first"},
		{name: "keyword past the read cap", target: Target{URL: srv.URL + "/big", Method: "GET", Expect: "2xx", Timeout: 10, Keyword: "needle"}, status: 200, errPart: "absent from first"},
		{name: "500 is not 2xx", target: Target{URL: srv.URL + "/500", Method: "GET", Expect: "2xx", Timeout: 5}, status: 500, errPart: "status 500, expected 2xx"},
		{name: "404 accepted when asked for", target: Target{URL: srv.URL + "/404", Method: "GET", Expect: "404", Timeout: 5}, wantOK: true, status: 404},
		{name: "HEAD", target: Target{URL: srv.URL + "/head", Method: "HEAD", Expect: "2xx", Timeout: 5}, wantOK: true, status: 204},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.Probe(context.Background(), tc.target)
			if got.OK != tc.wantOK {
				t.Fatalf("OK = %v, want %v (err %q)", got.OK, tc.wantOK, got.Err)
			}
			if got.Status != tc.status {
				t.Fatalf("status = %d, want %d", got.Status, tc.status)
			}
			if tc.errPart != "" && !strings.Contains(got.Err, tc.errPart) {
				t.Fatalf("error %q does not contain %q", got.Err, tc.errPart)
			}
			if tc.wantOK && got.Err != "" {
				t.Fatalf("healthy check carried an error: %q", got.Err)
			}
		})
	}
}

func TestProbeErrorsAreDistinct(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slow.Close()

	// A port nothing is listening on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := ln.Addr().String()
	ln.Close()

	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer redir.Close()

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := testChecker(t, s)

	cases := []struct {
		name   string
		target Target
		want   string
	}{
		{name: "dns", target: Target{URL: "https://this-host-does-not-exist.baileys.invalid/", Method: "GET", Expect: "2xx", Timeout: 5}, want: "dns: no such host"},
		{name: "refused", target: Target{URL: "http://" + deadAddr + "/", Method: "GET", Expect: "2xx", Timeout: 5}, want: "connection refused"},
		{name: "timeout", target: Target{URL: slow.URL + "/", Method: "GET", Expect: "2xx", Timeout: 1}, want: "timeout"},
		{name: "redirect loop", target: Target{URL: redir.URL + "/", Method: "GET", Expect: "2xx", Timeout: 5}, want: "too many redirects (>3)"},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.Probe(context.Background(), tc.target)
			if got.OK {
				t.Fatalf("expected a failure, got a pass")
			}
			if got.Err != tc.want {
				t.Fatalf("error = %q, want %q", got.Err, tc.want)
			}
			if prev, dup := seen[got.Err]; dup {
				t.Fatalf("error string %q is shared with %q — failures must read distinctly", got.Err, prev)
			}
			seen[got.Err] = tc.name
		})
	}
}

func TestTLSFailureIsDistinct(t *testing.T) {
	// httptest's TLS server uses a self-signed certificate the system does not trust.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := testChecker(t, s)
	got := c.Probe(context.Background(), Target{URL: srv.URL + "/", Method: "GET", Expect: "2xx", Timeout: 5})
	if got.OK {
		t.Fatal("an untrusted certificate must not pass")
	}
	if !strings.HasPrefix(got.Err, "tls:") {
		t.Fatalf("error = %q, want a tls-specific string", got.Err)
	}
}

// TestFlappingTargetOpensAndClosesIncident drives a real HTTP server that
// flips from healthy to broken and back, and asserts the derived incident.
func TestFlappingTargetOpensAndClosesIncident(t *testing.T) {
	var healthy sync.Mutex
	up := true
	setUp := func(v bool) { healthy.Lock(); up = v; healthy.Unlock() }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthy.Lock()
		defer healthy.Unlock()
		if up {
			fmt.Fprint(w, "ok")
			return
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	clk := &clock{t: t0}

	tg := mustTarget(t, s, TargetInput{Name: "flapper", URL: srv.URL + "/", Interval: "60", Timeout: "5"}, t0)
	c := testChecker(t, s)
	c.now = clk.now
	ctx := context.Background()

	// t+0: healthy.
	if chk := c.RunOne(ctx, tg); !chk.OK {
		t.Fatalf("first check should pass: %+v", chk)
	}
	if got := s.Incidents(tg.ID); len(got) != 0 {
		t.Fatalf("a healthy target opened %d incidents", len(got))
	}

	// t+60s: broken. This opens the incident.
	setUp(false)
	clk.set(t0.Add(60 * time.Second))
	if chk := c.RunOne(ctx, tg); chk.OK {
		t.Fatal("expected the 503 to fail the check")
	}
	incs := s.Incidents(tg.ID)
	if len(incs) != 1 {
		t.Fatalf("got %d incidents, want 1", len(incs))
	}
	if !incs[0].Open() {
		t.Fatal("incident should be open while the target is down")
	}
	if !incs[0].Start.Equal(t0.Add(60 * time.Second)) {
		t.Fatalf("incident started at %v, want %v", incs[0].Start, t0.Add(60*time.Second))
	}
	if !strings.Contains(incs[0].Err, "503") {
		t.Fatalf("incident cause %q does not name the status", incs[0].Err)
	}

	// t+120s: still broken. No second incident.
	clk.set(t0.Add(120 * time.Second))
	c.RunOne(ctx, tg)
	if got := s.Incidents(tg.ID); len(got) != 1 {
		t.Fatalf("a continuing outage opened %d incidents, want 1", len(got))
	}

	// t+180s: recovered. This closes the incident with a 120s duration.
	setUp(true)
	clk.set(t0.Add(180 * time.Second))
	if chk := c.RunOne(ctx, tg); !chk.OK {
		t.Fatalf("recovery check should pass: %+v", chk)
	}
	incs = s.Incidents(tg.ID)
	if len(incs) != 1 {
		t.Fatalf("got %d incidents, want 1", len(incs))
	}
	in := incs[0]
	if in.Open() {
		t.Fatal("incident should be closed after recovery")
	}
	if !in.End.Equal(t0.Add(180 * time.Second)) {
		t.Fatalf("incident ended at %v, want %v", *in.End, t0.Add(180*time.Second))
	}
	if got := in.Duration(clk.now()); got != 120*time.Second {
		t.Fatalf("incident duration = %v, want 2m0s", got)
	}
	if got := humanDuration(in.Duration(clk.now())); got != "2m 00s" {
		t.Fatalf("humanized duration = %q, want \"2m 00s\"", got)
	}

	// t+240s: broken again opens a second, separate incident.
	setUp(false)
	clk.set(t0.Add(240 * time.Second))
	c.RunOne(ctx, tg)
	incs = s.Incidents(tg.ID)
	if len(incs) != 2 {
		t.Fatalf("re-failing produced %d incidents, want 2", len(incs))
	}
	if !incs[0].Open() {
		t.Fatal("newest incident should be open and listed first")
	}

	// Five checks ran: pass, fail, fail, pass, fail.
	u := s.Uptime(tg.ID, time.Hour, t0.Add(240*time.Second))
	if u.Total != 5 || u.OK != 2 {
		t.Fatalf("uptime over the hour = %d/%d, want 2/5", u.OK, u.Total)
	}
	if got := fmtPct(u); got != "40.0%" {
		t.Fatalf("uptime reads %q, want 40.0%%", got)
	}
	// The 24h window must not include checks older than 24h.
	if late := s.Uptime(tg.ID, 24*time.Hour, t0.Add(25*time.Hour)); late.Known {
		t.Fatalf("24h window a day later still counts old checks: %+v", late)
	}
	// A one-second window ending before the run knows nothing at all.
	if before := s.Uptime(tg.ID, time.Second, t0.Add(-time.Hour)); before.Known {
		t.Fatalf("window before any check claims knowledge: %+v", before)
	}
}

func TestProbeNeverPanicsOut(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tg := mustTarget(t, s, TargetInput{Name: "panic", URL: "https://example.invalid/"}, now)

	c := testChecker(t, s)
	c.now = func() time.Time { return now }
	// A client whose transport panics stands in for any panicking check.
	c.client = &http.Client{Transport: panicTransport{}}

	chk := c.RunOne(context.Background(), tg)
	if chk.OK || chk.Err != "internal check error" {
		t.Fatalf("panic was not contained: %+v", chk)
	}
	if got := s.Checks(tg.ID); len(got) != 1 {
		t.Fatalf("the recovered check was not recorded: %d", len(got))
	}
}

type panicTransport struct{}

func (panicTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("transport exploded")
}
