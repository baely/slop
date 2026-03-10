package main

import (
	"log"
	"log/slog"
	"os"
	"strconv"

	"github.com/baely/staticer/internal/server"
	"github.com/baely/staticer/internal/storage"
	"github.com/baely/staticer/internal/temporal"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file if it exists (ignore error if not found)
	_ = godotenv.Load()

	// Set up structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load configuration from environment
	config := loadConfig()

	// Initialize storage
	store, err := storage.NewStorage(config.DatabasePath, config.SitesDir, logger)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Initialize Temporal client (optional - graceful degradation)
	var temporalClient *temporal.Client
	var temporalWorker *temporal.Worker

	temporalCfg := temporal.Config{
		Host:      getEnv("TEMPORAL_HOST", "temporal:7233"),
		Namespace: getEnv("TEMPORAL_NAMESPACE", "default"),
	}

	temporalClient, err = temporal.NewClient(temporalCfg, logger)
	if err != nil {
		logger.Warn("Failed to connect to Temporal, auto-deletion disabled", "error", err)
	} else {
		// Start the worker
		temporalWorker = temporal.NewWorker(temporalClient.GetClient(), store, logger)
		if err := temporalWorker.Start(); err != nil {
			logger.Error("Failed to start Temporal worker", "error", err)
			temporalClient.Close()
			temporalClient = nil
		}
	}

	// Cleanup on exit
	defer func() {
		if temporalWorker != nil {
			temporalWorker.Stop()
		}
		if temporalClient != nil {
			temporalClient.Close()
		}
	}()

	// Create server
	srv := server.New(config, store, logger, temporalClient)

	// Run server
	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// loadConfig loads configuration from environment variables
func loadConfig() *server.Config {
	return &server.Config{
		Port:              getEnv("SERVER_PORT", "8080"),
		Host:              getEnv("SERVER_HOST", "lab.baileys.app"),
		SitesDir:          getEnv("SITES_DIR", "./sites"),
		DatabasePath:      getEnv("DATABASE_PATH", "./data/staticer.db"),
		UploadSecret:      getEnv("UPLOAD_SECRET", ""),
		AdminSecret:       getEnv("ADMIN_SECRET", ""),
		MaxUploadSize:     getEnvInt64("MAX_UPLOAD_SIZE", 104857600),      // 100MB
		MaxExtractedSize:  getEnvInt64("MAX_EXTRACTED_SIZE", 524288000),   // 500MB
		MaxFilesPerSite:   getEnvInt("MAX_FILES_PER_SITE", 1000),
		RateLimitUploads:  getEnvInt("RATE_LIMIT_UPLOADS", 10),
		TLSEnabled:        getEnvBool("TLS_ENABLED", false),
		TLSCertCache:      getEnv("TLS_CERT_CACHE", "/var/cache/staticer"),
		TLSEmail:          getEnv("TLS_EMAIL", ""),
	}
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// getEnvInt64 gets an int64 environment variable with a default value
func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

// getEnvBool gets a boolean environment variable with a default value
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}
