# Traefik Deployment Guide

Complete guide for deploying Staticer with Traefik reverse proxy and automatic HTTPS.

## Why Traefik?

- **Automatic HTTPS**: Let's Encrypt integration with automatic certificate renewal
- **Wildcard certificates**: Single cert for `*.lab.baileys.app`
- **Docker integration**: Zero-config service discovery
- **Modern architecture**: HTTP/2, WebSocket support
- **Dashboard**: Web UI for monitoring

## Quick Start

### 1. DNS Configuration (Required First!)

Configure your DNS provider with these records:

```
lab.baileys.app          A      <your-server-ip>
*.lab.baileys.app        A      <your-server-ip>
traefik.lab.baileys.app  A      <your-server-ip>  (optional, for dashboard)
```

Verify DNS is propagated:
```bash
dig lab.baileys.app +short
dig test.lab.baileys.app +short
```

### 2. Generate Secrets

```bash
# Generate Staticer secrets
UPLOAD_SECRET=$(openssl rand -hex 32)
ADMIN_SECRET=$(openssl rand -hex 32)

echo "UPLOAD_SECRET=$UPLOAD_SECRET"
echo "ADMIN_SECRET=$ADMIN_SECRET"

# Generate Traefik dashboard password (username: admin)
docker run --rm httpd:alpine htpasswd -nb admin YourPassword123 | sed -e s/\\$/\\$\\$/g
```

### 3. Configure Environment

```bash
# Copy example file
cp .env.traefik.example .env

# Edit with your values
nano .env
```

Update these required values:
```bash
DOMAIN=lab.baileys.app
ACME_EMAIL=your-email@example.com
UPLOAD_SECRET=<from-step-2>
ADMIN_SECRET=<from-step-2>
TRAEFIK_DASHBOARD_AUTH=<from-step-2>
```

### 4. Deploy

```bash
# Pull latest image
docker pull registry.baileys.dev/staticer:latest

# Start services
docker compose -f docker-compose.traefik.yml up -d

# Watch logs
docker compose -f docker-compose.traefik.yml logs -f
```

### 5. Verify

```bash
# Check services are running
docker compose -f docker-compose.traefik.yml ps

# Test main domain (should redirect to HTTPS)
curl -I http://lab.baileys.app

# Test HTTPS
curl https://lab.baileys.app

# Access Traefik dashboard
open https://traefik.lab.baileys.app
# Login with: admin / YourPassword123
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                       Internet                          │
└───────────────────────┬─────────────────────────────────┘
                        │
                        │ Port 80/443
                        │
         ┌──────────────▼──────────────┐
         │        Traefik              │
         │  (Reverse Proxy + SSL)      │
         │                             │
         │  - lab.baileys.app          │
         │  - *.lab.baileys.app        │
         │  - Auto HTTPS (Let's Encrypt)│
         └──────────────┬──────────────┘
                        │
                        │ Port 8080
                        │
         ┌──────────────▼──────────────┐
         │     Staticer Server         │
         │                             │
         │  - Dashboard                │
         │  - API                      │
         │  - Static file serving      │
         └──────────────┬──────────────┘
                        │
         ┌──────────────┴──────────────┐
         │                             │
    ┌────▼─────┐              ┌────▼──────┐
    │ SQLite   │              │  Sites    │
    │ Database │              │ Directory │
    └──────────┘              └───────────┘
```

## Features Enabled

### Automatic HTTPS
- Let's Encrypt certificates
- Automatic renewal (60 days before expiry)
- HTTP to HTTPS redirect

### Wildcard Certificate
- Single certificate covers `*.lab.baileys.app`
- No per-subdomain certificate needed

### Security Headers
- HSTS (Strict-Transport-Security)
- Frame denial (X-Frame-Options: DENY)
- SSL redirect enforcement

### Monitoring
- Traefik dashboard at `https://traefik.lab.baileys.app`
- Access logs
- Health checks

## Configuration Details

### Traefik Labels Explained

**Main domain routing:**
```yaml
- "traefik.http.routers.staticer-main.rule=Host(`lab.baileys.app`)"
```
Routes `https://lab.baileys.app` to Staticer dashboard

**Wildcard routing:**
```yaml
- "traefik.http.routers.staticer-wildcard.rule=HostRegexp(`{subdomain:[a-z]+-[a-z]+}.lab.baileys.app`)"
```
Routes deployed sites like `https://happy-tree.lab.baileys.app`

**TLS configuration:**
```yaml
- "traefik.http.routers.staticer-main.tls.certresolver=letsencrypt"
- "traefik.http.routers.staticer-wildcard.tls.domains[0].main=lab.baileys.app"
- "traefik.http.routers.staticer-wildcard.tls.domains[0].sans=*.lab.baileys.app"
```
Enables HTTPS with wildcard certificate

## Common Operations

### View Logs

```bash
# All services
docker compose -f docker-compose.traefik.yml logs -f

# Just Traefik
docker compose -f docker-compose.traefik.yml logs -f traefik

# Just Staticer
docker compose -f docker-compose.traefik.yml logs -f staticer
```

### Restart Services

```bash
# Restart all
docker compose -f docker-compose.traefik.yml restart

# Restart Traefik (to reload config)
docker compose -f docker-compose.traefik.yml restart traefik

# Restart Staticer
docker compose -f docker-compose.traefik.yml restart staticer
```

### Update Staticer

```bash
# Pull latest image
docker pull registry.baileys.dev/staticer:latest

# Restart service
docker compose -f docker-compose.traefik.yml up -d staticer
```

### Check Certificate Status

```bash
# View certificate info
docker exec traefik cat /letsencrypt/acme.json | jq

# Check certificate expiry
echo | openssl s_client -servername lab.baileys.app -connect lab.baileys.app:443 2>/dev/null | openssl x509 -noout -dates
```

### Backup

```bash
# Backup certificates
docker run --rm -v traefik_traefik-letsencrypt:/data -v $(pwd)/backup:/backup alpine tar czf /backup/traefik-certs-$(date +%Y%m%d).tar.gz -C /data .

# Backup Staticer data
docker run --rm -v traefik_staticer-data:/data -v $(pwd)/backup:/backup alpine tar czf /backup/staticer-data-$(date +%Y%m%d).tar.gz -C /data .

# Backup sites
docker run --rm -v traefik_staticer-sites:/sites -v $(pwd)/backup:/backup alpine tar czf /backup/staticer-sites-$(date +%Y%m%d).tar.gz -C /sites .
```

## Troubleshooting

### Certificate Issues

**Problem:** Let's Encrypt rate limits hit

**Solution:** Use staging environment first
```bash
# In docker-compose.traefik.yml, change:
- --certificatesresolvers.letsencrypt.acme.caserver=https://acme-staging-v02.api.letsencrypt.org/directory
```

**Problem:** Certificate not issued

**Check:**
```bash
# View Traefik logs
docker compose -f docker-compose.traefik.yml logs traefik | grep -i acme

# Verify DNS is correct
dig lab.baileys.app +short
dig test.lab.baileys.app +short

# Check port 80 is accessible (required for HTTP challenge)
curl -I http://lab.baileys.app/.well-known/acme-challenge/test
```

### Wildcard Certificate Issues

**Problem:** Wildcard cert not covering subdomains

**Solution:** Ensure both main and wildcard domains are in TLS config:
```yaml
- "traefik.http.routers.staticer-wildcard.tls.domains[0].main=lab.baileys.app"
- "traefik.http.routers.staticer-wildcard.tls.domains[0].sans=*.lab.baileys.app"
```

**Note:** HTTP challenge doesn't support wildcards. For true wildcard support, use DNS challenge:
```bash
# Requires DNS provider API access (e.g., Cloudflare)
- --certificatesresolvers.letsencrypt.acme.dnschallenge=true
- --certificatesresolvers.letsencrypt.acme.dnschallenge.provider=cloudflare
```

### Service Not Reachable

```bash
# Check services are running
docker compose -f docker-compose.traefik.yml ps

# Check Traefik dashboard for routes
open https://traefik.lab.baileys.app

# Check Docker networks
docker network inspect traefik_traefik-network

# Test backend directly (bypass Traefik)
docker exec staticer-server wget -O- http://localhost:8080/
```

### Dashboard Not Accessible

**Problem:** 404 on `https://traefik.lab.baileys.app`

**Check:**
1. DNS record exists for `traefik.lab.baileys.app`
2. Dashboard auth is properly formatted (double `$$` for escaping)
3. Traefik container is healthy

```bash
docker compose -f docker-compose.traefik.yml ps traefik
```

## Security Best Practices

### 1. Strong Passwords

```bash
# Generate strong Traefik dashboard password
docker run --rm httpd:alpine htpasswd -nb admin $(openssl rand -base64 16) | sed -e s/\\$/\\$\\$/g

# Generate strong API secrets
openssl rand -hex 32
```

### 2. Restrict Dashboard Access

Add IP whitelist in docker-compose.traefik.yml:
```yaml
- "traefik.http.middlewares.dashboard-ipwhitelist.ipwhitelist.sourcerange=1.2.3.4/32"
- "traefik.http.routers.dashboard.middlewares=dashboard-auth,dashboard-ipwhitelist"
```

### 3. Security Headers

Already configured:
- HSTS with includeSubDomains
- Frame denial
- SSL redirect

### 4. Regular Updates

```bash
# Update Traefik
docker compose -f docker-compose.traefik.yml pull traefik
docker compose -f docker-compose.traefik.yml up -d traefik

# Update Staticer
docker pull registry.baileys.dev/staticer:latest
docker compose -f docker-compose.traefik.yml up -d staticer
```

## Performance Tuning

### Enable Compression

Add to Traefik command:
```yaml
- --entrypoints.websecure.http.middlewares=compress@docker
- --http.middlewares.compress.compress=true
```

### Connection Limits

Add to docker-compose.traefik.yml:
```yaml
deploy:
  resources:
    limits:
      cpus: '1.0'
      memory: 512M
```

## Monitoring

### Health Checks

```bash
# Check Traefik health
docker compose -f docker-compose.traefik.yml exec traefik traefik healthcheck

# Check Staticer health
docker compose -f docker-compose.traefik.yml exec staticer wget -O- http://localhost:8080/
```

### Metrics

Enable Prometheus metrics in Traefik:
```yaml
command:
  - --metrics.prometheus=true
  - --entryPoints.metrics.address=:8082
```

## Migration from Nginx

If migrating from the Nginx setup:

```bash
# 1. Stop Nginx compose
docker compose -f docker-compose.prod.yml down

# 2. Backup data
docker run --rm -v staticer_staticer-data:/data -v $(pwd):/backup alpine cp -r /data /backup/data-backup

# 3. Start Traefik compose (uses same volume names)
docker compose -f docker-compose.traefik.yml up -d

# 4. Verify
curl https://lab.baileys.app
```

## Cost

- **Traefik**: Free, open source
- **Let's Encrypt**: Free certificates
- **Hosting**: Your server costs only

## Support

- Traefik Docs: https://doc.traefik.io/traefik/
- Let's Encrypt: https://letsencrypt.org/docs/
- Staticer Issues: https://github.com/baely/staticer/issues
