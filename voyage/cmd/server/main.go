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

	"github.com/baileybutler/voyage/internal/server"
	"github.com/baileybutler/voyage/internal/store"
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

	if cfg.AdminToken == "" {
		log.Printf("warning: ADMIN_TOKEN is unset — the planner is open to anyone who can reach it")
	}

	srv := server.New(server.Options{
		Store:      db,
		Title:      cfg.Title,
		AdminToken: cfg.AdminToken,
		BaseURL:    cfg.BaseURL,
		Currency:   cfg.Currency,
		TrustProxy: cfg.TrustProxy,
	})

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		log.Printf("voyage listening on %s", cfg.Addr)
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
	Addr       string
	DBPath     string
	Title      string
	AdminToken string
	BaseURL    string
	Currency   string
	TrustProxy bool
}

func loadConfig() config {
	return config{
		Addr:       getenv("ADDR", ":8080"),
		DBPath:     getenv("DB_PATH", "/data/voyage.db"),
		Title:      getenv("TITLE", "Voyage"),
		AdminToken: os.Getenv("ADMIN_TOKEN"),
		BaseURL:    os.Getenv("BASE_URL"),
		Currency:   getenv("CURRENCY", "AUD"),
		TrustProxy: os.Getenv("TRUST_PROXY") == "1" || os.Getenv("TRUST_PROXY") == "true",
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
