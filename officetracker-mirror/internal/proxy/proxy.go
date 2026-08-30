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

// setCORS marks a response as readable from any origin. The mirror is
// anonymous and read-only — no cookies or credentials cross the boundary — so a
// wildcard origin is safe: there is no per-user data for the same-origin policy
// to protect, and the guard already rejects every write method.
func setCORS(h http.Header) {
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	h.Set("Access-Control-Max-Age", "86400")
}

// guard enforces the read-only contract before anything reaches the proxy.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capability endpoint: public, never proxied.
		if r.URL.Path == "/api/v1/meta" {
			writeMeta(w, r)
			return
		}

		if isDenied(r.URL.Path) {
			setCORS(w.Header())
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet, http.MethodHead:
			// Proxied responses get their CORS headers in modifyResponse.
			next.ServeHTTP(w, r)
		case http.MethodOptions:
			// CORS preflight: advertise the allowed methods/headers and stop.
			setCORS(w.Header())
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
	setCORS(w.Header())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(metaBody))
}

func writeForbidden(w http.ResponseWriter, r *http.Request) {
	setCORS(w.Header())
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

	// Replace any upstream CORS policy with our own so every proxied response
	// (HTML, JSON, static assets) is readable cross-origin.
	resp.Header.Del("Access-Control-Allow-Origin")
	resp.Header.Del("Access-Control-Allow-Methods")
	resp.Header.Del("Access-Control-Allow-Headers")
	setCORS(resp.Header)

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
