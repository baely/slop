package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func testServer(t *testing.T, content string) (*httptest.Server, string) {
	t.Helper()
	st, path := tempStore(t, content)
	h := &handler{store: st, base: "https://bly.au"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("POST /add", h.add)
	mux.HandleFunc("POST /update", h.update)
	mux.HandleFunc("POST /delete", h.delete)
	mux.HandleFunc("GET /generate", h.generate)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, path
}

func post(t *testing.T, srv *httptest.Server, path string, v url.Values) *http.Response {
	t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.PostForm(srv.URL+path, v)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestIndexListsLinks(t *testing.T) {
	srv, _ := testServer(t, "linkedin=https://linkedin.com/in/x\n=https://root.example\n")
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`href="https://bly.au/linkedin"`, `bly.au/linkedin`, `href="https://bly.au/"`, `value="https://linkedin.com/in/x"`, `<h1>hop.</h1>`, `b-glyph`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("index missing %s", want)
		}
	}
}

func TestAddWithKey(t *testing.T) {
	srv, path := testServer(t, "")
	resp := post(t, srv, "/add", url.Values{"key": {"gh"}, "url": {"https://github.com/baely"}})
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "gh=https://github.com/baely\n" {
		t.Fatalf("file: %q", got)
	}
}

func TestAddBlankKeyGenerates(t *testing.T) {
	srv, path := testServer(t, "")
	resp := post(t, srv, "/add", url.Values{"key": {""}, "url": {"https://x.example"}, "len": {"5"}, "upper": {"1"}})
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	got, _ := os.ReadFile(path)
	key, _, _ := strings.Cut(strings.TrimSpace(string(got)), "=")
	if len(key) != 5 || strings.ToUpper(key) != key {
		t.Fatalf("generated key %q does not follow the rules", key)
	}
}

func TestAddErrorsRenderInline(t *testing.T) {
	srv, _ := testServer(t, "gh=https://github.com/baely\n")
	resp := post(t, srv, "/add", url.Values{"key": {"gh"}, "url": {"https://other.example"}})
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "already exists") || !strings.Contains(string(body), `value="https://other.example"`) {
		t.Fatalf("status %d body:\n%s", resp.StatusCode, body)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	srv, path := testServer(t, "a=https://a.example\nb=https://b.example\n")
	if resp := post(t, srv, "/update", url.Values{"key": {"a"}, "url": {"https://a2.example"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update: %d", resp.StatusCode)
	}
	if resp := post(t, srv, "/delete", url.Values{"key": {"b"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a=https://a2.example\n" {
		t.Fatalf("file: %q", got)
	}
}

func TestGenerateEndpoint(t *testing.T) {
	srv, _ := testServer(t, "")
	resp, err := http.Get(srv.URL + "/generate?len=4&digits=1&unambiguous=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(body) != 4 || strings.ContainsAny(string(body), "01abc") {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	resp, _ = http.Get(srv.URL + "/generate?len=4")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no classes should be 400, got %d", resp.StatusCode)
	}
	resp, _ = http.Get(srv.URL + "/generate")
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || len(body) != defaultRules.Length {
		t.Fatalf("defaults: %d %q", resp.StatusCode, body)
	}
}
