.PHONY: help build build-server build-cli run test clean install dev init-db docker-build docker-up docker-down docker-logs docker-prod-up docker-prod-down docker-push docker-build-push traefik-up traefik-down traefik-logs

.DEFAULT_GOAL := help

help: ## Show this help message
	@echo "Staticer - Static Site Hosting Platform"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: build-server build-cli ## Build both server and CLI binaries

build-server: ## Build server binary
	@echo "Building server..."
	@go build -o staticer-server ./cmd/server
	@echo "✓ Server built: staticer-server"

build-cli: ## Build CLI binary
	@echo "Building CLI..."
	@go build -o staticer ./cmd/staticer
	@echo "✓ CLI built: staticer"

run: ## Run the server in development mode
	@echo "Starting server..."
	@go run ./cmd/server

test: ## Run all tests
	@echo "Running tests..."
	@go test -v ./...

clean: ## Remove build artifacts and test data
	@echo "Cleaning build artifacts..."
	@rm -f staticer-server staticer
	@rm -rf sites/*
	@rm -f data/staticer.db*
	@echo "✓ Clean complete"

install: ## Install CLI globally
	@echo "Installing CLI globally..."
	@go install ./cmd/staticer
	@echo "✓ CLI installed"

dev: ## Run with auto-reload (requires air)
	@echo "Starting development server with auto-reload..."
	@air -c .air.toml || go run ./cmd/server

init-db: ## Initialize database and directories
	@echo "Initializing directories..."
	@mkdir -p data sites
	@touch data/.gitkeep sites/.gitkeep
	@echo "✓ Directories initialized"

# Docker compose command (supports both docker-compose and docker compose)
DOCKER_COMPOSE := $(shell which docker-compose 2>/dev/null || echo "docker compose")

# Docker targets
docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@$(DOCKER_COMPOSE) build
	@echo "✓ Docker image built"

docker-up: ## Start services with Docker Compose (development)
	@echo "Starting Docker services..."
	@$(DOCKER_COMPOSE) up -d
	@echo "✓ Services started at http://localhost:8080"

docker-down: ## Stop Docker services
	@echo "Stopping Docker services..."
	@$(DOCKER_COMPOSE) down
	@echo "✓ Services stopped"

docker-logs: ## View Docker logs
	@$(DOCKER_COMPOSE) logs -f

docker-restart: ## Restart Docker services
	@echo "Restarting Docker services..."
	@$(DOCKER_COMPOSE) restart
	@echo "✓ Services restarted"

docker-clean: ## Stop services and remove volumes
	@echo "Cleaning Docker resources..."
	@$(DOCKER_COMPOSE) down -v
	@echo "✓ Volumes removed"

docker-prod-up: ## Start services in production mode
	@echo "Starting production services..."
	@$(DOCKER_COMPOSE) -f docker-compose.prod.yml up -d
	@echo "✓ Production services started"

docker-prod-down: ## Stop production services
	@echo "Stopping production services..."
	@$(DOCKER_COMPOSE) -f docker-compose.prod.yml down
	@echo "✓ Production services stopped"

docker-backup: ## Backup Docker volumes
	@echo "Backing up volumes..."
	@mkdir -p backups
	@docker run --rm -v staticer-data:/data -v $$(pwd)/backups:/backup alpine tar czf /backup/staticer-data-$$(date +%Y%m%d_%H%M%S).tar.gz -C /data .
	@docker run --rm -v staticer-sites:/sites -v $$(pwd)/backups:/backup alpine tar czf /backup/staticer-sites-$$(date +%Y%m%d_%H%M%S).tar.gz -C /sites .
	@echo "✓ Backup complete in ./backups/"

# Registry configuration
REGISTRY := registry.baileys.dev
IMAGE_NAME := staticer
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "latest")
FULL_IMAGE := $(REGISTRY)/$(IMAGE_NAME):$(VERSION)
LATEST_IMAGE := $(REGISTRY)/$(IMAGE_NAME):latest
PLATFORM := linux/amd64

docker-build-registry: ## Build Docker image for registry (linux/amd64)
	@echo "Building image for registry ($(PLATFORM))..."
	@docker build --platform $(PLATFORM) -t $(FULL_IMAGE) -t $(LATEST_IMAGE) .
	@echo "✓ Built: $(FULL_IMAGE)"
	@echo "✓ Built: $(LATEST_IMAGE)"

docker-push: ## Push Docker image to registry
	@echo "Pushing to registry..."
	@docker push $(FULL_IMAGE)
	@docker push $(LATEST_IMAGE)
	@echo "✓ Pushed: $(FULL_IMAGE)"
	@echo "✓ Pushed: $(LATEST_IMAGE)"

docker-build-push: docker-build-registry docker-push ## Build and push to registry

docker-build-multiarch: ## Build and push multi-architecture image
	@echo "Building multi-architecture image..."
	@docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(FULL_IMAGE) \
		-t $(LATEST_IMAGE) \
		--push \
		.
	@echo "✓ Built and pushed multi-arch: $(FULL_IMAGE)"

# Traefik targets
traefik-up: ## Start services with Traefik (production with HTTPS)
	@echo "Starting Traefik + Staticer..."
	@$(DOCKER_COMPOSE) -f docker-compose.traefik.yml up -d
	@echo "✓ Services started with Traefik"
	@echo "  Dashboard: https://$(DOMAIN)"
	@echo "  Traefik: https://traefik.$(DOMAIN)"

traefik-down: ## Stop Traefik services
	@echo "Stopping Traefik services..."
	@$(DOCKER_COMPOSE) -f docker-compose.traefik.yml down
	@echo "✓ Traefik services stopped"

traefik-logs: ## View Traefik logs
	@$(DOCKER_COMPOSE) -f docker-compose.traefik.yml logs -f

traefik-restart: ## Restart Traefik services
	@echo "Restarting Traefik services..."
	@$(DOCKER_COMPOSE) -f docker-compose.traefik.yml restart
	@echo "✓ Traefik services restarted"
