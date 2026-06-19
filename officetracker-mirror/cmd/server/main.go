// Command server runs an anonymous, read-only mirror of officetracker.com.au.
//
// It is a stateless reverse proxy (a Backend-for-Frontend): every upstream
// request is authenticated with a single API token tied to the operator's
// account, so anonymous visitors see the real UI and read API without logging
// in. All write/update endpoints are rejected and sensitive management
// surfaces are hidden.
package main

import (
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/baileybutler/officetracker-mirror/internal/proxy"
)

func main() {
	addr := envOr("ADDR", ":8080")
	upstreamRaw := envOr("UPSTREAM", "https://officetracker.com.au")

	token := os.Getenv("OFFICETRACKER_TOKEN")
	if token == "" {
		log.Fatal("OFFICETRACKER_TOKEN is required (value: officetracker:<64 chars>)")
	}

	upstream, err := url.Parse(upstreamRaw)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		log.Fatalf("invalid UPSTREAM %q: %v", upstreamRaw, err)
	}

	handler := proxy.New(proxy.Config{Upstream: upstream, Token: token})

	mux := http.NewServeMux()
	// Local health endpoint for container checks; never proxied upstream.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/", handler)

	log.Printf("officetracker-mirror listening on %s -> %s", addr, upstream)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
