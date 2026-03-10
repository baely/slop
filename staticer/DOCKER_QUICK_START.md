# Docker Quick Start

Get Staticer running with Docker in 60 seconds.

## Prerequisites

- Docker installed and running
- Docker Compose (comes with Docker Desktop)

## Start Now

```bash
# Clone and enter directory
cd /path/to/staticer

# Start services
make docker-up

# Or manually
docker compose up -d
```

Access: `http://localhost:8080`

**Test credentials:**
- Upload Secret: `test-secret-123`
- Admin Secret: `admin-secret-456`

## Common Commands

```bash
# View logs
make docker-logs

# Restart services
make docker-restart

# Stop services
make docker-down

# Stop and remove all data
make docker-clean

# Backup volumes
make docker-backup
```

## What's Running

| Service | Port | Description |
|---------|------|-------------|
| Staticer Server | 8080 | Main application |

## Volumes

| Volume | Purpose | Path in Container |
|--------|---------|-------------------|
| `staticer-data` | SQLite database | `/app/data` |
| `staticer-sites` | Deployed sites | `/app/sites` |

## Quick Test

### 1. Upload via Web Dashboard

1. Open http://localhost:8080
2. Enter secret: `test-secret-123`
3. Click "Save Secret"
4. Drag and drop a ZIP file (must contain `index.html`)
5. Your site will be deployed to a random subdomain

### 2. Upload via CLI

```bash
# Build CLI
make build-cli

# Configure
./staticer config --secret test-secret-123 --url http://localhost:8080

# Deploy
cd /path/to/your/site
../staticer deploy
```

### 3. Upload via API

```bash
# Create test ZIP
cd /tmp
echo '<h1>Hello World</h1>' > index.html
zip test-site.zip index.html

# Deploy
curl -X POST http://localhost:8080/api/deploy \
  -H 'X-Upload-Secret: test-secret-123' \
  -F 'file=@test-site.zip'
```

## Production Setup

For production with SSL and strong secrets:

```bash
# 1. Create production environment file
cp .env.docker.example .env

# 2. Generate secrets
openssl rand -hex 32  # Use for UPLOAD_SECRET
openssl rand -hex 32  # Use for ADMIN_SECRET

# 3. Edit .env with real secrets and domain
nano .env

# 4. Start production services
make docker-prod-up
```

## Troubleshooting

### Port 8080 already in use

```bash
# Change port in docker-compose.yml
ports:
  - "8081:8080"  # Use 8081 instead
```

### Permission denied

```bash
# On Linux, ensure Docker group
sudo usermod -aG docker $USER
newgrp docker
```

### Container keeps restarting

```bash
# Check logs
make docker-logs

# Common issues:
# - Invalid secrets in .env
# - Database locked (another instance running)
```

### Volume not persisting

```bash
# Check volumes exist
docker volume ls | grep staticer

# Inspect volume
docker volume inspect staticer-data
```

## Next Steps

- Read [DOCKER.md](DOCKER.md) for complete documentation
- Read [README.md](README.md) for feature overview
- Read [TESTING.md](TESTING.md) for testing instructions

## Architecture

```
┌─────────────────────────────────────────┐
│           Docker Host                   │
│                                         │
│  ┌────────────────────────────────┐   │
│  │     staticer-server            │   │
│  │     (Go application)           │   │
│  │     Port: 8080                 │   │
│  └──────────┬──────────┬──────────┘   │
│             │          │               │
│             │          │               │
│     ┌───────▼─────┐  ┌▼──────────┐   │
│     │staticer-data│  │staticer-  │   │
│     │  (volume)   │  │  sites    │   │
│     │             │  │ (volume)  │   │
│     │ SQLite DB   │  │ Static    │   │
│     │             │  │ Files     │   │
│     └─────────────┘  └───────────┘   │
│                                        │
└────────────────────────────────────────┘
         │
         │ Port 8080
         │
    ┌────▼────┐
    │ Browser │
    │   or    │
    │   CLI   │
    └─────────┘
```

## File Overview

```
/Users/bailey/projects/staticer/
├── Dockerfile              # Multi-stage build
├── docker-compose.yml      # Development config
├── docker-compose.prod.yml # Production config with Nginx
├── .dockerignore          # Build optimization
├── nginx.conf             # Nginx reverse proxy config
└── .env.docker.example    # Environment template
```

## Health Check

The container includes automatic health checks:

```bash
# Check if container is healthy
docker ps

# Manual health check
curl http://localhost:8080/
```

## Data Persistence

**Volumes persist data across container restarts.**

To completely reset:

```bash
# Stop and remove everything
make docker-clean

# Start fresh
make docker-up
```

## Support

- GitHub: https://github.com/baely/staticer
- Issues: https://github.com/baely/staticer/issues
