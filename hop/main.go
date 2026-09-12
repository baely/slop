// Command hop is a URL shortener that answers from pre-serialised HTTP
// responses over a bare TCP listener. Links come from a text file of
// "key=url" lines.
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	logger := log.New(os.Stderr, "", 0)

	linksPath := env("LINKS", "links.txt")
	cfg := Config{
		Addr:           env("ADDR", ":8080"),
		HeadTimeout:    envDuration("HEAD_TIMEOUT", 5*time.Second),
		IdleTimeout:    envDuration("IDLE_TIMEOUT", 10*time.Second),
		WriteTimeout:   envDuration("WRITE_TIMEOUT", 5*time.Second),
		MaxHeadBytes:   envInt("MAX_HEAD_BYTES", 8192),
		MaxConns:       envInt("MAX_CONNS", 4096),
		MaxConnsPerIP:  envInt("MAX_CONNS_PER_IP", 0),
		RedirectStatus: envInt("REDIRECT_STATUS", 302),
	}
	if _, ok := reasons[cfg.RedirectStatus]; !ok || cfg.RedirectStatus/100 != 3 {
		logger.Fatalf("hop: REDIRECT_STATUS must be one of 301, 302, 307, 308 (got %d)", cfg.RedirectStatus)
	}

	load := func() (*table, int, error) {
		links, err := LoadLinks(linksPath)
		if err != nil {
			return nil, 0, err
		}
		return buildTable(links, cfg.RedirectStatus), len(links), nil
	}

	t, count, err := load()
	if err != nil {
		logger.Fatalf("hop: %v", err)
	}
	srv := NewServer(cfg, t, logger)

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		logger.Fatalf("hop: %v", err)
	}
	logger.Printf("hop: %d links from %s, redirecting with %d, listening on %s", count, linksPath, cfg.RedirectStatus, ln.Addr())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for s := range sig {
			if s == syscall.SIGHUP {
				t, count, err := load()
				if err != nil {
					logger.Printf("hop: reload failed, keeping previous links: %v", err)
					continue
				}
				srv.SetTable(t)
				logger.Printf("hop: reloaded %d links", count)
				continue
			}
			logger.Printf("hop: %s, shutting down", s)
			ln.Close()
		}
	}()

	if err := srv.Serve(ln); err != nil {
		logger.Fatalf("hop: %v", err)
	}
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "hop: %s must be a non-negative integer (got %q)\n", name, v)
		os.Exit(2)
	}
	return n
}

func envDuration(name string, def time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "hop: %s must be a positive duration like 5s (got %q)\n", name, v)
		os.Exit(2)
	}
	return d
}
