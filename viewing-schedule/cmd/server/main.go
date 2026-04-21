package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/baileybutler/viewing-schedule/internal/letterboxd"
	"github.com/baileybutler/viewing-schedule/internal/server"
	"github.com/baileybutler/viewing-schedule/internal/store"
	syncpkg "github.com/baileybutler/viewing-schedule/internal/sync"
	"github.com/baileybutler/viewing-schedule/internal/tmdb"
)

func main() {
	cfg := loadConfig()

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	tm := tmdb.New(cfg.TMDBToken)
	lb := letterboxd.New()

	var sync *syncpkg.Service
	if cfg.LetterboxdUser != "" {
		var err error
		sync, err = syncpkg.New(syncpkg.Options{
			Store:      db,
			Letterboxd: lb,
			TMDB:       tm,
			User:       cfg.LetterboxdUser,
			Interval:   cfg.SyncInterval,
		})
		if err != nil {
			log.Fatalf("init sync: %v", err)
		}
	}

	srv := server.New(server.Options{
		Store:      db,
		TMDB:       tm,
		Letterboxd: lb,
		Sync:       sync,
		Title:      cfg.Title,
		DateRange:  cfg.DateRange,
		AdminToken: cfg.AdminToken,
		TrustProxy: cfg.TrustProxy,
	})

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if sync != nil {
		log.Printf("sync: configured for letterboxd user %q (interval %s)", cfg.LetterboxdUser, cfg.SyncInterval)
		sync.Start(ctx)
	}

	go func() {
		log.Printf("viewing-schedule listening on %s", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = httpSrv.Shutdown(shutCtx)
}

type config struct {
	Addr           string
	DBPath         string
	TMDBToken      string
	Title          string
	DateRange      string
	AdminToken     string
	TrustProxy     bool
	LetterboxdUser string
	SyncInterval   time.Duration
}

func loadConfig() config {
	c := config{
		Addr:           getenv("ADDR", ":8080"),
		DBPath:         getenv("DB_PATH", "/data/viewing.db"),
		TMDBToken:      os.Getenv("TMDB_TOKEN"),
		Title:          getenv("TITLE", "Viewing Schedule"),
		DateRange:      os.Getenv("DATE_RANGE"),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		TrustProxy:     os.Getenv("TRUST_PROXY") == "1" || os.Getenv("TRUST_PROXY") == "true",
		LetterboxdUser: os.Getenv("LETTERBOXD_USER"),
		SyncInterval:   parseDuration(getenv("SYNC_INTERVAL", "6h")),
	}
	return c
}

// parseDuration parses a time.Duration string. Returns 0 (disabled) on parse
// error or for the literal "0".
func parseDuration(s string) time.Duration {
	if s == "" || s == "0" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("warn: invalid SYNC_INTERVAL %q, periodic sync disabled", s)
		return 0
	}
	return d
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
