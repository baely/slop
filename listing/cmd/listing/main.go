package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/baely/listing/internal/docker"
	"github.com/baely/listing/internal/server"
	"github.com/baely/listing/internal/staticer"
	"github.com/baely/listing/internal/store"
)

func main() {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Initialize container store
	containerStore := store.New()

	// Initialize Docker client
	cli, err := docker.NewClient()
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	// Scan existing containers
	ctx := context.Background()
	if err := docker.ScanContainers(ctx, cli, containerStore); err != nil {
		log.Fatalf("Failed to scan containers: %v", err)
	}

	// Start event listener in background
	eventCtx, eventCancel := context.WithCancel(ctx)
	go docker.ListenEvents(eventCtx, cli, containerStore)

	// Initialize staticer client (optional)
	var staticerClient *staticer.Client
	if staticerURL := os.Getenv("STATICER_URL"); staticerURL != "" {
		staticerClient = staticer.NewClient(staticerURL)
		staticerClient.Start(30 * time.Second)
		log.Printf("Staticer integration enabled, polling %s", staticerURL)
	}

	// Initialize HTTP server
	srv, err := server.New(addr, containerStore, staticerClient)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start HTTP server in background
	go func() {
		log.Printf("Starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")

	// Stop event listener
	eventCancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}
