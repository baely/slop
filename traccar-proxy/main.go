// traccar-proxy accepts position reports from the Traccar client (OsmAnd
// HTTP protocol) and redistributes each one to several upstream services.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var queues []*Queue
	var workers []*Worker
	for _, t := range cfg.Targets {
		q, err := openQueue(filepath.Join(cfg.DataDir, "queue", t.Name), cfg.MaxQueue)
		if err != nil {
			log.Fatalf("queue %s: %v", t.Name, err)
		}
		w := newWorker(t, q, cfg.Timeout)
		queues = append(queues, q)
		workers = append(workers, w)
		go w.Run(ctx)
		log.Printf("target %-12s → %s (%d queued)", t.Name, t.Redacted(), q.Len())
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newServer(cfg, queues, workers),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	if cfg.Token != "" {
		log.Printf("listening on %s (client path /%s)", cfg.Addr, cfg.Token)
	} else {
		log.Printf("listening on %s", cfg.Addr)
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
