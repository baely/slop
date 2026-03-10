# Docker Deployment Guide

Complete guide for running Staticer with Docker and Docker Compose.

## Quick Start

### Development (Local)

```bash
# Start the server
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the server
docker-compose down

# Stop and remove volumes (clean slate)
docker-compose down -v
```

Access the dashboard at `http://localhost:8080`

Default credentials:
- Upload Secret: `test-secret-123`
- Admin Secret: `admin-secret-456`

### Production

1. **Generate secrets**:
```bash
cp .env.docker.example .env

# Generate strong secrets
UPLOAD_SECRET=$(openssl rand -hex 32)
ADMIN_SECRET=$(openssl rand -hex 32)

# Edit .env and paste the secrets
nano .env
```

2. **Start with production config**:
```bash
docker-compose -f docker-compose.prod.yml up -d
```

## Docker Compose Files

### docker-compose.yml
- For local development
- Hardcoded test secrets
- Runs on port 8080
- Simple setup, no SSL

### docker-compose.prod.yml
- For production deployment
- Reads secrets from .env file
- Includes optional Nginx reverse proxy
- Production-grade logging
- Restart policy: always

## Volumes

Two persistent volumes are created:

1. **staticer-data**: SQLite database storage
   - Path inside container: `/app/data`
   - Contains: `staticer.db`

2. **staticer-sites**: Deployed site files
   - Path inside container: `/app/sites`
   - Contains: All uploaded static sites

### Volume Management

**Backup volumes**:
```bash
# Backup database
docker run --rm \
  -v staticer-data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/staticer-data-backup.tar.gz -C /data .

# Backup sites
docker run --rm \
  -v staticer-sites:/sites \
  -v $(pwd):/backup \
  alpine tar czf /backup/staticer-sites-backup.tar.gz -C /sites .
```

**Restore volumes**:
```bash
# Restore database
docker run --rm \
  -v staticer-data:/data \
  -v $(pwd):/backup \
  alpine sh -c "cd /data && tar xzf /backup/staticer-data-backup.tar.gz"

# Restore sites
docker run --rm \
  -v staticer-sites:/sites \
  -v $(pwd):/backup \
  alpine sh -c "cd /sites && tar xzf /backup/staticer-sites-backup.tar.gz"
```

**Inspect volumes**:
```bash
# List volumes
docker volume ls

# Inspect volume
docker volume inspect staticer-data

# Browse volume contents
docker run --rm -v staticer-data:/data alpine ls -la /data
```

## Building

### Build image manually

```bash
# Build
docker build -t staticer:latest .

# Run
docker run -d \
  -p 8080:8080 \
  -e SERVER_HOST=localhost \
  -e UPLOAD_SECRET=test-secret-123 \
  -e ADMIN_SECRET=admin-secret-456 \
  -v staticer-data:/app/data \
  -v staticer-sites:/app/sites \
  --name staticer-server \
  staticer:latest
```

### Multi-architecture build

```bash
# Build for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t staticer:latest \
  --push .
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | `8080` | Port to listen on |
| `SERVER_HOST` | `localhost` | Primary hostname |
| `SITES_DIR` | `/app/sites` | Site storage directory |
| `DATABASE_PATH` | `/app/data/staticer.db` | SQLite database path |
| `UPLOAD_SECRET` | *required* | Shared secret for uploads |
| `ADMIN_SECRET` | *required* | Admin API secret |
| `MAX_UPLOAD_SIZE` | `104857600` | Max ZIP size (100MB) |
| `MAX_EXTRACTED_SIZE` | `524288000` | Max extracted size (500MB) |
| `MAX_FILES_PER_SITE` | `1000` | Max files per site |
| `RATE_LIMIT_UPLOADS` | `10` | Uploads per hour per IP |

### Override environment variables

```bash
# In docker-compose.yml
environment:
  - UPLOAD_SECRET=my-custom-secret

# Or via .env file
echo "UPLOAD_SECRET=my-custom-secret" > .env
docker-compose up -d
```

## Production Setup with Nginx

The production compose file includes an optional Nginx reverse proxy.

### Enable SSL

1. **Get SSL certificates**:
```bash
# Using certbot
sudo certbot certonly --standalone \
  -d lab.baileys.app \
  -d "*.lab.baileys.app"

# Copy certificates
mkdir -p ssl
sudo cp /etc/letsencrypt/live/lab.baileys.app/fullchain.pem ssl/cert.pem
sudo cp /etc/letsencrypt/live/lab.baileys.app/privkey.pem ssl/key.pem
```

2. **Update nginx.conf**:
   - Uncomment the HTTPS server block
   - Uncomment the HTTP→HTTPS redirect
   - Update certificate paths if needed

3. **Restart**:
```bash
docker-compose -f docker-compose.prod.yml restart nginx
```

### Nginx features
- HTTP/2 support
- Gzip compression
- Rate limiting for uploads
- Security headers
- SSL/TLS termination
- Log rotation

## Health Checks

The container includes a health check that pings the root URL every 30 seconds.

**Check health status**:
```bash
docker ps
# Look for "healthy" status

# Detailed health check
docker inspect --format='{{json .State.Health}}' staticer-server | jq
```

## Logging

**View logs**:
```bash
# Follow logs
docker-compose logs -f

# View specific service
docker-compose logs -f staticer

# Last 100 lines
docker-compose logs --tail=100 staticer
```

**Production logging** (in docker-compose.prod.yml):
- JSON format logs
- Max 10MB per file
- Keep last 3 files

## Monitoring

**Resource usage**:
```bash
# Container stats
docker stats staticer-server

# Disk usage
docker system df
```

**Admin API monitoring**:
```bash
# Get statistics
curl http://localhost:8080/api/admin/stats \
  -H "X-Admin-Secret: your-admin-secret"
```

## Troubleshooting

### Container won't start

```bash
# Check logs
docker-compose logs staticer

# Check if port is in use
sudo lsof -i :8080

# Restart with fresh build
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Permission issues

```bash
# Fix volume permissions
docker-compose down
docker volume rm staticer-data staticer-sites
docker-compose up -d
```

### Database locked

```bash
# Stop all containers accessing the database
docker-compose down

# Start fresh
docker-compose up -d
```

### Upload fails

```bash
# Check container logs
docker-compose logs staticer | grep -i error

# Verify secrets
docker-compose exec staticer env | grep SECRET

# Check disk space
docker-compose exec staticer df -h
```

### Site not accessible

```bash
# Test directly (bypass Nginx)
curl -H "Host: test.localhost" http://localhost:8080/

# Check Nginx logs
docker-compose logs nginx

# Verify site was deployed
docker-compose exec staticer ls -la /app/sites/
```

## Development Workflow

### Live development

For development with live reload, mount your source code:

```yaml
services:
  staticer:
    build: .
    volumes:
      - ./cmd:/app/cmd
      - ./internal:/app/internal
      - ./web:/app/web
    command: go run cmd/server/main.go
```

### Run tests in container

```bash
# Run unit tests
docker-compose exec staticer go test ./...

# Run with coverage
docker-compose exec staticer go test -coverprofile=coverage.out ./...
docker-compose exec staticer go tool cover -html=coverage.out
```

## Security Best Practices

1. **Never commit secrets**
   - Use `.env` files (in `.gitignore`)
   - Generate strong random secrets
   - Rotate secrets regularly

2. **Run as non-root**
   - Container runs as user `staticer` (UID 1000)
   - Volumes owned by container user

3. **Network isolation**
   - Use internal Docker network
   - Only expose necessary ports

4. **Update regularly**
   - Pull latest Alpine base image
   - Rebuild containers periodically

5. **Monitor logs**
   - Set up log aggregation
   - Alert on errors

## Maintenance

### Update the application

```bash
# Pull latest code
git pull

# Rebuild and restart
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Backup automation

Create a backup script:

```bash
#!/bin/bash
# backup.sh

DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/backups/staticer"

mkdir -p $BACKUP_DIR

# Backup volumes
docker run --rm \
  -v staticer-data:/data \
  -v $BACKUP_DIR:/backup \
  alpine tar czf /backup/staticer-data-$DATE.tar.gz -C /data .

docker run --rm \
  -v staticer-sites:/sites \
  -v $BACKUP_DIR:/backup \
  alpine tar czf /backup/staticer-sites-$DATE.tar.gz -C /sites .

# Keep only last 7 days
find $BACKUP_DIR -name "staticer-*" -mtime +7 -delete

echo "Backup completed: $DATE"
```

Add to crontab:
```bash
# Daily backup at 2 AM
0 2 * * * /path/to/backup.sh >> /var/log/staticer-backup.log 2>&1
```

## Migration from Non-Docker

If you have an existing Staticer installation:

1. **Backup existing data**:
```bash
cp -r /path/to/staticer/data ./data-backup
cp -r /path/to/staticer/sites ./sites-backup
```

2. **Copy to Docker volumes**:
```bash
# Start containers to create volumes
docker-compose up -d
docker-compose down

# Copy data
docker run --rm \
  -v staticer-data:/data \
  -v $(pwd)/data-backup:/backup \
  alpine sh -c "cp -r /backup/* /data/"

docker run --rm \
  -v staticer-sites:/sites \
  -v $(pwd)/sites-backup:/backup \
  alpine sh -c "cp -r /backup/* /sites/"

# Start with migrated data
docker-compose up -d
```

## Support

- Issues: https://github.com/baely/staticer/issues
- Documentation: README.md
- Testing: TESTING.md
