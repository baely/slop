package main

import (
	"bufio"
	"bytes"
	"errors"
	"github.com/baely/slop/hop/links"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testLinks = []links.Link{
	{Path: "/linkedin", URL: "https://linkedin.com/in/baileybutler1"},
	{Path: "/docs/api", URL: "https://example.com/docs?x=1"},
}

func testConfig() Config {
	return Config{
		HeadTimeout:  300 * time.Millisecond,
		IdleTimeout:  500 * time.Millisecond,
		WriteTimeout: time.Second,
		MaxHeadBytes: 1024,
		MaxConns:     8,
	}
}

// start runs a server on a loopback port and returns its address.
func start(t *testing.T, cfg Config) (*Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(cfg, buildTable(testLinks), log.New(io.Discard, "", 0))
	go s.Serve(ln)
	t.Cleanup(func() { ln.Close() })
	return s, ln.Addr().String()
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(5 * time.Second))
	return c
}

// readResponse parses one response off the wire with a GET semantics reader.
func readResponse(t *testing.T, r *bufio.Reader, method string) *http.Response {
	t.Helper()
	resp, err := http.ReadResponse(r, &http.Request{Method: method})
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp
}

func send(t *testing.T, addr, raw string) *http.Response {
	t.Helper()
	c := dial(t, addr)
	if _, err := io.WriteString(c, raw); err != nil {
		t.Fatal(err)
	}
	return readResponse(t, bufio.NewReader(c), strings.Fields(raw)[0])
}

func TestRedirects(t *testing.T) {
	_, addr := start(t, testConfig())
	cases := []struct {
		path, location string
	}{
		{"/linkedin", "https://linkedin.com/in/baileybutler1"},
		{"/linkedin?utm_source=x&utm_medium=y", "https://linkedin.com/in/baileybutler1"},
		{"/docs/api", "https://example.com/docs?x=1"},
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, c := range cases {
		resp, err := client.Get("http://" + addr + c.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 302 || resp.Header.Get("Location") != c.location {
			t.Errorf("%s: got %d %q", c.path, resp.StatusCode, resp.Header.Get("Location"))
		}
		if resp.ContentLength != 0 {
			t.Errorf("%s: redirect has body of %d bytes", c.path, resp.ContentLength)
		}
	}
}

func TestPages(t *testing.T) {
	_, addr := start(t, testConfig())
	client := &http.Client{}
	check := func(path string, status int, contains string) {
		t.Helper()
		resp, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != status {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, status)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("%s: content-type %q", path, ct)
		}
		if !strings.Contains(string(body), contains) {
			t.Errorf("%s: body missing %q", path, contains)
		}
	}
	check("/", 200, "<h1>hop.</h1>")
	check("/nope", 404, "HTTP 404")
	check("/linkedin/extra", 404, "HTTP 404")
	check("/LinkedIn", 404, "HTTP 404") // exact bytes only
	check("/linkedin/", 404, "HTTP 404")
}

func TestRootLinkReplacesIndex(t *testing.T) {
	s, addr := start(t, testConfig())
	s.SetTable(buildTable([]links.Link{{Path: "/", URL: "https://baileybutler.com"}}))
	resp := send(t, addr, "GET /?ref=x HTTP/1.1\r\nHost: h\r\n\r\n")
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "https://baileybutler.com" {
		t.Fatalf("got %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// The swapped table has no linkedin.
	if resp := send(t, addr, "GET /linkedin HTTP/1.1\r\n\r\n"); resp.StatusCode != 404 {
		t.Fatalf("after swap: got %d", resp.StatusCode)
	}
}

func TestHead(t *testing.T) {
	_, addr := start(t, testConfig())
	c := dial(t, addr)
	io.WriteString(c, "HEAD /nope HTTP/1.1\r\nConnection: close\r\n\r\n")
	raw, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(raw, []byte("\r\n\r\n")) {
		t.Fatalf("HEAD response carried a body:\n%s", raw)
	}
	if !bytes.HasPrefix(raw, []byte("HTTP/1.1 404 Not Found\r\n")) || !bytes.Contains(raw, []byte("Content-Length: ")) {
		t.Fatalf("unexpected HEAD response:\n%s", raw)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	_, addr := start(t, testConfig())
	resp := send(t, addr, "POST /linkedin HTTP/1.1\r\nContent-Length: 3\r\n\r\nabc")
	if resp.StatusCode != 405 || resp.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("got %d allow=%q", resp.StatusCode, resp.Header.Get("Allow"))
	}
	if !resp.Close {
		t.Errorf("request with body should force Connection: close")
	}
}

func TestBadRequest(t *testing.T) {
	_, addr := start(t, testConfig())
	c := dial(t, addr)
	io.WriteString(c, "not http at all\r\n\r\n")
	raw, _ := io.ReadAll(c)
	if !bytes.HasPrefix(raw, []byte("HTTP/1.1 400 Bad Request\r\n")) || !bytes.Contains(raw, []byte("Connection: close")) {
		t.Fatalf("got:\n%s", raw)
	}
}

func TestKeepAliveAndPipelining(t *testing.T) {
	_, addr := start(t, testConfig())
	c := dial(t, addr)
	r := bufio.NewReader(c)

	// Two requests, two round trips, one connection.
	io.WriteString(c, "GET /linkedin HTTP/1.1\r\nHost: h\r\n\r\n")
	if resp := readResponse(t, r, "GET"); resp.StatusCode != 302 || resp.Close {
		t.Fatalf("first: %d close=%v", resp.StatusCode, resp.Close)
	}
	io.WriteString(c, "GET /nope HTTP/1.1\r\nHost: h\r\n\r\n")
	if resp := readResponse(t, r, "GET"); resp.StatusCode != 404 {
		t.Fatalf("second: %d", resp.StatusCode)
	}

	// Three pipelined requests in one write, plus a partial fourth.
	io.WriteString(c, "GET /docs/api HTTP/1.1\r\n\r\nHEAD / HTTP/1.1\r\n\r\nGET /linkedin HTTP/1.1\r\nConnection: close\r\n\r\nGET /part")
	want := []int{302, 200, 302}
	for i, method := range []string{"GET", "HEAD", "GET"} {
		if resp := readResponse(t, r, method); resp.StatusCode != want[i] {
			t.Fatalf("pipelined %d: got %d want %d", i, resp.StatusCode, want[i])
		}
	}
	if _, err := r.ReadByte(); err != io.EOF {
		t.Fatalf("expected EOF after Connection: close, got %v", err)
	}
}

func TestHTTP10Closes(t *testing.T) {
	_, addr := start(t, testConfig())
	c := dial(t, addr)
	io.WriteString(c, "GET /linkedin HTTP/1.0\r\n\r\n")
	raw, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("Connection: close\r\n")) {
		t.Fatalf("HTTP/1.0 response should close:\n%s", raw)
	}
}

func TestSlowlorisHeadTimeout(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	started := time.Now()
	// Trickle a header forever, one byte every 50ms, never finishing.
	done := make(chan struct{})
	go func() {
		defer close(done)
		io.WriteString(c, "GET /linkedin HTTP/1.1\r\nX-Slow: ")
		for i := 0; i < 200; i++ {
			if _, err := io.WriteString(c, "a"); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	raw, _ := io.ReadAll(c)
	elapsed := time.Since(started)
	<-done
	if !bytes.HasPrefix(raw, []byte("HTTP/1.1 408 Request Timeout\r\n")) {
		t.Fatalf("expected 408, got:\n%s", raw)
	}
	if elapsed < cfg.HeadTimeout || elapsed > cfg.HeadTimeout*4 {
		t.Fatalf("closed after %v, want about %v", elapsed, cfg.HeadTimeout)
	}
}

func TestSlowlorisAfterKeepAlive(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	r := bufio.NewReader(c)
	io.WriteString(c, "GET /linkedin HTTP/1.1\r\n\r\n")
	readResponse(t, r, "GET")
	// Idle for a bit, then start a request and stall: the head timeout must
	// kick in from the first byte, not the idle timeout.
	time.Sleep(cfg.IdleTimeout / 2)
	started := time.Now()
	io.WriteString(c, "GET /li")
	raw, _ := io.ReadAll(r)
	if !bytes.HasPrefix(raw, []byte("HTTP/1.1 408")) {
		t.Fatalf("expected 408, got:\n%s", raw)
	}
	if el := time.Since(started); el < cfg.HeadTimeout || el > cfg.HeadTimeout*3 {
		t.Fatalf("closed after %v, want about %v", el, cfg.HeadTimeout)
	}
}

func TestIdleTimeoutSilent(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	r := bufio.NewReader(c)
	io.WriteString(c, "GET /linkedin HTTP/1.1\r\n\r\n")
	readResponse(t, r, "GET")
	started := time.Now()
	rest, err := io.ReadAll(r)
	if err != nil || len(rest) != 0 {
		t.Fatalf("idle close should be silent, got %q %v", rest, err)
	}
	if el := time.Since(started); el < cfg.IdleTimeout || el > cfg.IdleTimeout*3 {
		t.Fatalf("closed after %v, want about %v", el, cfg.IdleTimeout)
	}
}

func TestHeadersTooLarge(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	c := dial(t, addr)
	io.WriteString(c, "GET /linkedin HTTP/1.1\r\nX-Big: "+strings.Repeat("a", cfg.MaxHeadBytes)+"\r\n\r\n")
	raw, _ := io.ReadAll(c)
	if !bytes.HasPrefix(raw, []byte("HTTP/1.1 431 ")) || !bytes.Contains(raw, []byte("Connection: close")) {
		t.Fatalf("got:\n%s", raw)
	}
}

func TestMaxConns(t *testing.T) {
	cfg := testConfig()
	_, addr := start(t, cfg)
	// Fill every slot with a connection that has sent nothing.
	for i := 0; i < cfg.MaxConns; i++ {
		dial(t, addr)
	}
	c := dial(t, addr)
	raw, _ := io.ReadAll(c)
	if !bytes.HasPrefix(raw, []byte("HTTP/1.1 503 ")) || !bytes.Contains(raw, []byte("Retry-After: 1")) {
		t.Fatalf("got:\n%s", raw)
	}
	// Once the idle ones time out, service resumes.
	time.Sleep(cfg.HeadTimeout * 2)
	if resp := send(t, addr, "GET /linkedin HTTP/1.1\r\n\r\n"); resp.StatusCode != 302 {
		t.Fatalf("after recovery: %d", resp.StatusCode)
	}
}

func TestNetHTTPClientKeepAlive(t *testing.T) {
	// The standard client reuses connections; make sure a burst of requests
	// through it all succeed, exercising the keep-alive loop under a real
	// client.
	_, addr := start(t, testConfig())
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for i := 0; i < 50; i++ {
		resp, err := client.Get("http://" + addr + "/linkedin?i=" + strings.Repeat("x", i))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 302 {
			t.Fatalf("request %d: %d", i, resp.StatusCode)
		}
	}
}

func TestListenerClosedStopsServe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(testConfig(), buildTable(nil), log.New(io.Discard, "", 0))
	errc := make(chan error, 1)
	go func() { errc <- s.Serve(ln) }()
	ln.Close()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after listener closed")
	}
}
