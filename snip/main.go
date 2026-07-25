package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type config struct {
	Addr    string
	DataDir string
	BaseURL string
	Token   string
}

func loadConfig() config {
	c := config{
		Addr:    env("ADDR", ":8080"),
		DataDir: env("DATA_DIR", "/data"),
		BaseURL: strings.TrimRight(os.Getenv("BASE_URL"), "/"),
		Token:   os.Getenv("APP_TOKEN"),
	}
	if c.BaseURL == "" {
		c.BaseURL = "http://localhost" + c.Addr
	}
	return c
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetFlags(0)
	cfg := loadConfig()

	if cfg.Token == "" {
		log.Println("snip: APP_TOKEN is empty. Set APP_TOKEN to a long random secret and restart. Refusing to start without it.")
		os.Exit(1)
	}

	store, err := NewStore(cfg.DataDir + "/pastes")
	if err != nil {
		log.Fatalf("snip: cannot open data dir %s: %v", cfg.DataDir, err)
	}
	log.Printf("snip: loaded %d pastes from %s", store.Len(), cfg.DataDir)

	srv, err := newServer(cfg, store)
	if err != nil {
		log.Fatalf("snip: startup: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Background sweeper. The read path also checks expiry, so this is only
	// about reclaiming disk, never about correctness.
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n := store.Sweep(); n > 0 {
					log.Printf("snip: swept %d expired pastes", n)
				}
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	go func() {
		log.Printf("snip: listening on %s, base url %s", cfg.Addr, cfg.BaseURL)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("snip: listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("snip: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("snip: shutdown: %v", err)
	}
}
