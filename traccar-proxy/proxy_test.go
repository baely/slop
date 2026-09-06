package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func mustURL(t *testing.T, s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestBuildURL(t *testing.T) {
	cases := []struct{ base, path, query, want string }{
		{"http://h:5055", "", "id=1&lat=2", "http://h:5055?id=1&lat=2"},
		{"http://h:5055/", "/", "id=1&lat=2", "http://h:5055/?id=1&lat=2"},
		{"https://ha.local/api/webhook/abc", "", "id=1&lat=2", "https://ha.local/api/webhook/abc?id=1&lat=2"},
		{"https://ha.local/api/webhook/abc/", "/extra/path", "", "https://ha.local/api/webhook/abc/extra/path"},
		// target's fixed params win over the client's
		{"http://h/?id=phone", "", "lat=2&id=1&lon=3", "http://h/?lat=2&lon=3&id=phone"},
		{"http://h/?token=t", "", "id=1", "http://h/?id=1&token=t"},
		{"http://h/?token=t", "", "", "http://h/?token=t"},
		// client order and encoding are preserved untouched
		{"http://h/", "", "z=1&a=2&x=a%20b", "http://h/?z=1&a=2&x=a%20b"},
		{"http://user:pw@h/", "", "id=1", "http://user:pw@h/?id=1"},
	}
	for _, c := range cases {
		got := buildURL(mustURL(t, c.base), &Job{Path: c.path, Query: c.query}).String()
		if got != c.want {
			t.Errorf("buildURL(%q, %q, %q) = %q, want %q", c.base, c.path, c.query, got, c.want)
		}
	}
}

func TestStripToken(t *testing.T) {
	s := &Server{cfg: Config{Token: "s3cret"}}
	cases := []struct {
		in   string
		rest string
		ok   bool
	}{
		{"/s3cret", "", true},
		{"/s3cret/", "/", true},
		{"/s3cret/api/status", "/api/status", true},
		{"/", "", false},
		{"/s3cretx", "", false},
		{"/other/s3cret", "", false},
	}
	for _, c := range cases {
		rest, ok := s.stripToken(c.in)
		if rest != c.rest || ok != c.ok {
			t.Errorf("stripToken(%q) = %q,%v want %q,%v", c.in, rest, ok, c.rest, c.ok)
		}
	}
	open := &Server{cfg: Config{}}
	if rest, ok := open.stripToken("/anything"); !ok || rest != "/anything" {
		t.Errorf("no token should pass everything through, got %q,%v", rest, ok)
	}
}

func TestQueuePersistsAndEvicts(t *testing.T) {
	dir := t.TempDir()
	q, err := openQueue(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := q.Push(&Job{Received: time.Now(), Method: "POST", Query: "n=" + string(rune('0'+i))}); err != nil {
			t.Fatal(err)
		}
	}
	if q.Len() != 3 || q.dropped.Load() != 2 {
		t.Fatalf("len=%d dropped=%d, want 3/2", q.Len(), q.dropped.Load())
	}
	// Reopen from disk: same three, oldest first.
	q2, err := openQueue(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if q2.Len() != 3 {
		t.Fatalf("reopened len=%d", q2.Len())
	}
	j, name, _ := q2.Peek()
	if j == nil || j.Query != "n=2" {
		t.Fatalf("head = %+v, want n=2", j)
	}
	q2.Ack(name)
	if j, _, _ := q2.Peek(); j == nil || j.Query != "n=3" {
		t.Fatalf("after ack head = %+v, want n=3", j)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 2 {
		t.Fatalf("%d files on disk, want 2", len(files))
	}
}

// recorder is a fake upstream that captures requests and can be told what
// status to return.
type recorder struct {
	mu     sync.Mutex
	got    []*http.Request
	bodies []string
	status atomic.Int32
}

func newRecorder(status int) (*recorder, *httptest.Server) {
	r := &recorder{}
	r.status.Store(int32(status))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, req)
		r.bodies = append(r.bodies, string(b))
		r.mu.Unlock()
		w.WriteHeader(int(r.status.Load()))
	}))
	return r, srv
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFanOut(t *testing.T) {
	okRec, okSrv := newRecorder(200)
	defer okSrv.Close()
	flakyRec, flakySrv := newRecorder(503)
	defer flakySrv.Close()
	badRec, badSrv := newRecorder(400)
	defer badSrv.Close()

	cfg := Config{
		DataDir:  t.TempDir(),
		Token:    "tok",
		Timeout:  2 * time.Second,
		MaxQueue: 100,
		Targets: []Target{
			{Name: "ok", URL: mustURL(t, okSrv.URL+"/")},
			{Name: "flaky", URL: mustURL(t, flakySrv.URL+"/hook?id=renamed")},
			{Name: "bad", URL: mustURL(t, badSrv.URL)},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var queues []*Queue
	var workers []*Worker
	for _, tg := range cfg.Targets {
		q, err := openQueue(filepath.Join(cfg.DataDir, "queue", tg.Name), cfg.MaxQueue)
		if err != nil {
			t.Fatal(err)
		}
		w := newWorker(tg, q, cfg.Timeout)
		queues = append(queues, q)
		workers = append(workers, w)
		go w.Run(ctx)
	}
	proxy := httptest.NewServer(newServer(cfg, queues, workers))
	defer proxy.Close()

	// Wrong path is rejected without touching any target.
	resp, _ := http.Post(proxy.URL+"/?id=1&lat=1&lon=2", "", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("untokened request status %d, want 404", resp.StatusCode)
	}

	// What the Traccar Android client sends.
	q := "id=123456&timestamp=1757145600&lat=-37.8136&lon=144.9631&speed=0.0&bearing=0.0&altitude=31.0&accuracy=12.0&batt=87.0"
	resp, err := http.Post(proxy.URL+"/tok?"+q, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("ingest status %d", resp.StatusCode)
	}
	// And a JSON-body variant, as newer clients may send.
	resp, _ = http.Post(proxy.URL+"/tok/", "application/json", strings.NewReader(`{"location":{"coords":{"latitude":1,"longitude":2}}}`))
	if resp.StatusCode != 200 {
		t.Fatalf("json ingest status %d", resp.StatusCode)
	}

	waitFor(t, "ok target to get both", func() bool { return okRec.count() == 2 })
	waitFor(t, "bad target to see both and drop them", func() bool { return badRec.count() == 2 && queues[2].Len() == 0 })
	waitFor(t, "flaky target to be attempted", func() bool { return flakyRec.count() >= 1 })

	okRec.mu.Lock()
	first := okRec.got[0]
	if first.Method != "POST" || first.URL.RawQuery != q || first.URL.Path != "/" {
		t.Errorf("ok target got %s %s?%s", first.Method, first.URL.Path, first.URL.RawQuery)
	}
	if okRec.bodies[1] == "" || okRec.got[1].Header.Get("Content-Type") != "application/json" {
		t.Errorf("json body not replayed: %q %q", okRec.bodies[1], okRec.got[1].Header.Get("Content-Type"))
	}
	okRec.mu.Unlock()

	flakyRec.mu.Lock()
	fq := flakyRec.got[0].URL.Query()
	if flakyRec.got[0].URL.Path != "/hook" || fq.Get("id") != "renamed" || fq.Get("lat") != "-37.8136" {
		t.Errorf("flaky target got %s?%s", flakyRec.got[0].URL.Path, flakyRec.got[0].URL.RawQuery)
	}
	flakyRec.mu.Unlock()

	// Flaky target is backing off with both jobs still queued; ok target is clear.
	if queues[1].Len() != 2 || queues[0].Len() != 0 {
		t.Fatalf("queues: ok=%d flaky=%d", queues[0].Len(), queues[1].Len())
	}
	snap := workers[1].Snapshot()
	if snap.State != "retrying" || !strings.HasPrefix(snap.LastError, "503") {
		t.Errorf("flaky snapshot %+v", snap)
	}
	if bs := workers[2].Snapshot(); bs.Rejected != 2 || bs.State != "ok" {
		t.Errorf("bad snapshot %+v", bs)
	}

	// Recover: queue drains in order.
	flakyRec.status.Store(200)
	waitFor(t, "flaky queue to drain", func() bool { return queues[1].Len() == 0 })
	flakyRec.mu.Lock()
	n := len(flakyRec.got)
	last := flakyRec.bodies[n-1]
	flakyRec.mu.Unlock()
	if !strings.Contains(last, "latitude") {
		t.Errorf("last delivery to flaky was not the JSON job: %q", last)
	}
	if s := workers[1].Snapshot(); s.State != "ok" || s.Delivered != 2 {
		t.Errorf("flaky after recovery %+v", s)
	}

	// Status endpoints.
	resp, _ = http.Get(proxy.URL + "/tok/api/status")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"name": "flaky"`) {
		t.Errorf("status json: %d %s", resp.StatusCode, body)
	}
	resp, _ = http.Get(proxy.URL + "/tok")
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "<h1>traccar-proxy</h1>") || !strings.Contains(string(body), proxy.URL+"/tok") {
		t.Errorf("status page: %d", resp.StatusCode)
	}
	resp, _ = http.Get(proxy.URL + "/healthz")
	if resp.StatusCode != 200 {
		t.Errorf("healthz %d", resp.StatusCode)
	}
	// Nothing leaked to targets from the status calls.
	if okRec.count() != 2 {
		t.Errorf("ok target received %d, want 2", okRec.count())
	}
}

func TestLoadConfig(t *testing.T) {
	os.Clearenv()
	t.Setenv("TARGET_Traccar", "https://t.example.com:5055")
	t.Setenv("TARGET_ha", "https://ha.example.com/api/webhook/x?id=phone")
	t.Setenv("TARGET_empty", "")
	t.Setenv("TOKEN", "/abc/")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 || cfg.Targets[0].Name != "ha" || cfg.Targets[1].Name != "traccar" {
		t.Fatalf("targets %+v", cfg.Targets)
	}
	if cfg.Token != "abc" {
		t.Errorf("token %q", cfg.Token)
	}
	if got := cfg.Targets[0].Redacted(); got != "https://ha.example.com/api/webhook/x?id=…" {
		t.Errorf("redacted %q", got)
	}
	t.Setenv("TARGET_bad", "not a url")
	if _, err := loadConfig(); err == nil {
		t.Error("expected error for bad target url")
	}
	os.Clearenv()
	if _, err := loadConfig(); err == nil {
		t.Error("expected error with no targets")
	}
}
