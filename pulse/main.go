package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Config is the whole runtime configuration, all from env.
type Config struct {
	Addr    string
	DataDir string
	BaseURL string
	Token   string
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	cfg := Config{
		Addr:    envOr("ADDR", ":8080"),
		DataDir: envOr("DATA_DIR", "/data"),
		BaseURL: strings.TrimSuffix(envOr("BASE_URL", ""), "/"),
		Token:   os.Getenv("APP_TOKEN"),
	}
	if cfg.Token == "" {
		logger.Println("fatal: APP_TOKEN is empty; refusing to start without a shared secret")
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logger.Printf("fatal: cannot create DATA_DIR %s: %v", cfg.DataDir, err)
		os.Exit(1)
	}

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		logger.Printf("fatal: cannot load state: %v", err)
		os.Exit(1)
	}
	logger.Printf("loaded %d targets from %s", store.Count(), cfg.DataDir)

	checker := NewChecker(store, logger, 16)
	app := &App{
		cfg:     cfg,
		store:   store,
		auth:    NewAuth(cfg.Token),
		checker: checker,
		logger:  logger,
		secure:  !strings.HasPrefix(cfg.BaseURL, "http://"),
	}
	if err := app.parseTemplates(); err != nil {
		logger.Printf("fatal: templates: %v", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go checker.Run(rootCtx)

	// Persistence is debounced: checks mark the store dirty and a flusher
	// writes at most every few seconds, so a busy estate does not rewrite
	// the file once per probe.
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-rootCtx.Done():
				return
			case <-t.C:
				if err := store.SaveIfDirty(); err != nil {
					logger.Printf("persist error: %v", err)
				}
			}
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          logger,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s (data=%s)", cfg.Addr, cfg.DataDir)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Printf("fatal: server: %v", err)
			if serr := store.Save(); serr != nil {
				logger.Printf("persist error on exit: %v", serr)
			}
			os.Exit(1)
		}
	case <-rootCtx.Done():
		logger.Println("shutdown signal received")
		shutCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logger.Printf("shutdown: %v", err)
		}
		if err := store.Save(); err != nil {
			logger.Printf("persist error on exit: %v", err)
		}
		logger.Println("stopped")
	}
}
