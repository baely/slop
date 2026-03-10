# Staticer Deployment Quick Reference

## ✅ Current Status

**Docker Image Built & Pushed:**
- Registry: `registry.baileys.dev/staticer:latest`
- Platform: `linux/amd64`
- Size: 37.1 MB
- Status: Ready for deployment

## 🚀 Three Deployment Options

### 1. Development (Local Testing)

```bash
make docker-up
# Access: http://localhost:8080
# Secrets: test-secret-123 / admin-secret-456
```

### 2. Production with Traefik (⭐ RECOMMENDED)

**Features:**
- ✅ Automatic HTTPS (Let's Encrypt)
- ✅ Wildcard certificates (`*.lab.baileys.app`)
- ✅ Auto HTTP→HTTPS redirect
- ✅ Zero-config SSL renewal
- ✅ Traefik dashboard

**Setup:**
```bash
# 1. Configure DNS
lab.baileys.app          A      <your-ip>
*.lab.baileys.app        A      <your-ip>

# 2. Generate secrets
cp .env.traefik.example .env
# Edit DOMAIN, ACME_EMAIL, UPLOAD_SECRET, ADMIN_SECRET

# 3. Deploy
make traefik-up

# Access:
# https://lab.baileys.app (Staticer)
# https://traefik.lab.baileys.app (Traefik dashboard)
```

**Docs:** [TRAEFIK_DEPLOYMENT.md](TRAEFIK_DEPLOYMENT.md)

### 3. Production (Manual/Nginx)

```bash
# 1. Configure environment
cp .env.docker.example .env

# 2. Deploy
make docker-prod-up

# Access: http://your-ip:8080
# (You need to configure Nginx/reverse proxy separately)
```

**Docs:** [DOCKER.md](DOCKER.md)

## 🔧 Build Commands

### Build for linux/amd64 (default)
```bash
make docker-build-registry
```

### Push to registry
```bash
make docker-push
```

### Build & Push (one command)
```bash
make docker-build-push
```

### Multi-architecture (amd64 + arm64)
```bash
make docker-build-multiarch
```

## 📦 Pull & Run on Server

```bash
# Pull image
docker pull registry.baileys.dev/staticer:latest

# Run with docker-compose
docker compose -f docker-compose.traefik.yml up -d

# Or run directly
docker run -d \
  -p 8080:8080 \
  -e SERVER_HOST=lab.baileys.app \
  -e UPLOAD_SECRET=your-secret \
  -e ADMIN_SECRET=your-admin-secret \
  -v staticer-data:/app/data \
  -v staticer-sites:/app/sites \
  registry.baileys.dev/staticer:latest
```

## 🔐 Generate Secrets

```bash
# Staticer secrets
openssl rand -hex 32

# Traefik dashboard password
docker run --rm httpd:alpine htpasswd -nb admin YourPassword | sed -e s/\\$/\\$\\$/g
```

## 📊 Common Commands

### View Logs
```bash
make docker-logs              # Docker Compose
make traefik-logs             # Traefik setup
```

### Restart Services
```bash
make docker-restart           # Docker Compose
make traefik-restart          # Traefik setup
```

### Stop Everything
```bash
make docker-down              # Docker Compose
make traefik-down             # Traefik setup
```

### Clean Reset
```bash
make docker-clean             # Remove volumes too
```

### Backup
```bash
make docker-backup            # Saves to ./backups/
```

## 📁 Files Overview

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Development setup |
| `docker-compose.traefik.yml` | **Production with Traefik (recommended)** |
| `docker-compose.prod.yml` | Production with manual proxy |
| `.env.traefik.example` | Traefik environment template |
| `.env.docker.example` | Docker environment template |
| `Dockerfile` | Image build definition |
| `Makefile` | Build & deploy commands |

## 🆘 Troubleshooting

### Check if running
```bash
docker ps | grep staticer
```

### Test API
```bash
curl http://localhost:8080/api/sites \
  -H 'X-Upload-Secret: test-secret-123'
```

### View container logs
```bash
docker logs staticer-server -f
```

### Restart container
```bash
docker restart staticer-server
```

### Check volumes
```bash
docker volume ls | grep staticer
```

## 🌐 DNS Requirements

**Required records:**
```
lab.baileys.app          A      <your-server-ip>
*.lab.baileys.app        A      <your-server-ip>
```

**Verify DNS:**
```bash
dig lab.baileys.app +short
dig test.lab.baileys.app +short
```

## 📚 Documentation

- **[TRAEFIK_DEPLOYMENT.md](TRAEFIK_DEPLOYMENT.md)** - Traefik setup guide (recommended)
- **[DOCKER.md](DOCKER.md)** - Complete Docker documentation
- **[DOCKER_QUICK_START.md](DOCKER_QUICK_START.md)** - 60-second quick start
- **[README.md](README.md)** - Project overview
- **[TESTING.md](TESTING.md)** - Testing instructions

## ⚡ Production Checklist

- [ ] DNS configured (A and wildcard A records)
- [ ] Strong secrets generated (`openssl rand -hex 32`)
- [ ] `.env` file configured
- [ ] Docker installed on server
- [ ] Image pulled: `docker pull registry.baileys.dev/staticer:latest`
- [ ] Services started: `make traefik-up`
- [ ] HTTPS working (check https://lab.baileys.app)
- [ ] Test upload via web dashboard
- [ ] Test subdomain routing (deploy a test site)
- [ ] Backup configured

## 🎯 Next Steps

1. **For development:** `make docker-up`
2. **For production:** Follow [TRAEFIK_DEPLOYMENT.md](TRAEFIK_DEPLOYMENT.md)
3. **To deploy:** SSH to server and run commands above

---

**Image:** `registry.baileys.dev/staticer:latest`
**Platform:** `linux/amd64`
**Size:** 37.1 MB
**Status:** ✅ Ready to deploy
