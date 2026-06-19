package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// Config holds the upstream target and the API token injected into every
// proxied request.
type Config struct {
	Upstream *url.URL
	Token    string
}

// deniedPrefixes are paths blocked entirely (token & account management, SSO
// flows, and the MCP tool RPC which exposes a write tool). Matched as the exact
// path or as a "<prefix>/..." subpath.
var deniedPrefixes = []string{
	"/api/v1/developer",    // create/list/revoke API secrets
	"/api/v1/account/link", // mints an Auth0 account-linking URL
	"/mcp",                 // tool RPC, includes set_day (write)
	"/auth",                // SSO / OAuth callbacks
}

// deniedExact are individual HTML routes that only make sense for the real,
// authenticated owner.
var deniedExact = map[string]bool{
	"/login":     true,
	"/logout":    true,
	"/developer": true,
}

// New returns the read-only mirror handler: a guard (method + path filtering)
// wrapping a reverse proxy that injects the API token and rewrites responses.
func New(cfg Config) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = cfg.Upstream.Scheme
			req.URL.Host = cfg.Upstream.Host
			req.Host = cfg.Upstream.Host

			// Drop any client-supplied credentials and inject ours, so every
			// anonymous visitor is served the configured account's data.
			req.Header.Del("Cookie")
			req.Header.Set("Authorization", "Bearer "+cfg.Token)

			// Strip Accept-Encoding so Go's transport transparently decodes the
			// response body and ModifyResponse can rewrite plain HTML.
			req.Header.Del("Accept-Encoding")
		},
		ModifyResponse: modifyResponse,
	}

	return guard(rp)
}

// metaBody advertises the server's mode to clients (e.g. the mobile app):
// anonymous auth and read-only. Served locally and unauthenticated, since the
// upstream has no such route. Clients treat auth=="none" as "skip login,
// connect with an empty token" and read_only==true as "lock the UI".
const metaBody = `{"auth":"none","read_only":true}`

// guard enforces the read-only contract before anything reaches the proxy.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capability endpoint: public, never proxied.
		if r.URL.Path == "/api/v1/meta" {
			writeMeta(w, r)
			return
		}

		if isDenied(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet, http.MethodHead:
			next.ServeHTTP(w, r)
		case http.MethodOptions:
			w.WriteHeader(http.StatusNoContent)
		default:
			writeForbidden(w, r)
		}
	})
}

func isDenied(path string) bool {
	if deniedExact[path] {
		return true
	}
	for _, p := range deniedPrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func writeMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeForbidden(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(metaBody))
}

func writeForbidden(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/mcp/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":403,"message":"read-only mirror"}`))
		return
	}
	http.Error(w, "read-only mirror", http.StatusForbidden)
}

// modifyResponse strips account cookies and injects the read-only UI assets
// into HTML pages.
func modifyResponse(resp *http.Response) error {
	// Never leak the upstream account's session cookie to an anonymous client.
	resp.Header.Del("Set-Cookie")

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if cerr := resp.Body.Close(); cerr != nil {
		return cerr
	}

	body = injectReadOnly(body)

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}
