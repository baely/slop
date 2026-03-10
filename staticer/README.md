# Staticer - Static Site Hosting Platform

Deploy static sites to random subdomain URLs at **lab.baileys.app**.

## Features

- 📦 Upload sites as ZIP files
- 🎲 Random subdomain generation (e.g., `happy-tree.lab.baileys.app`)
- 🌐 Web dashboard for management
- 💻 CLI tool for command-line deployments
- 🔒 Secure with shared secret authentication
- 🗑️ Easy deletion with API keys
- 👨‍💼 Admin API for monitoring and management

## Quick Start

### Docker (Recommended)

The easiest way to run Staticer is with Docker:

```bash
# Development (with test secrets)
make docker-up

# Or manually
docker-compose up -d
```

Access the dashboard at `http://localhost:8080`

Default credentials:
- Upload Secret: `test-secret-123`
- Admin Secret: `admin-secret-456`

**Production with Traefik (Recommended):**

Automatic HTTPS with Let's Encrypt and wildcard certificates:

```bash
# Configure DNS first (A records for domain and *.domain)
# Generate secrets and configure .env
cp .env.traefik.example .env
nano .env

# Start with Traefik reverse proxy
make traefik-up
```

See [TRAEFIK_DEPLOYMENT.md](TRAEFIK_DEPLOYMENT.md) for complete Traefik guide.

**Production (manual):**
```bash
# Generate secrets
cp .env.docker.example .env
# Edit .env with strong secrets

# Start services
make docker-prod-up
```

See [DOCKER.md](DOCKER.md) for complete Docker documentation.

### Server Setup (Native)

1. **Configure DNS** (required before running):
   ```
   lab.baileys.app          A      <your-server-ip>
   *.lab.baileys.app        A      <your-server-ip>
   ```

2. **Set up environment**:
   ```bash
   cp .env.example .env
   # Edit .env and set your secrets
   ```

3. **Run the server**:
   ```bash
   make build-server
   ./staticer-server
   ```

### CLI Usage

1. **Install the CLI**:
   ```bash
   make build-cli
   sudo mv staticer /usr/local/bin/
   ```

2. **Configure**:
   ```bash
   staticer config --secret your-upload-secret
   ```

3. **Deploy a site**:
   ```bash
   cd your-static-site
   staticer deploy
   ```

4. **Manage sites**:
   ```bash
   staticer list              # List your sites
   staticer delete happy-tree # Delete a site
   ```

### Web Dashboard

Open `https://lab.baileys.app` in your browser and:
1. Enter your upload secret
2. Drag and drop a ZIP file
3. Get your site URL instantly

## API Reference

### Deploy Site
```http
POST /api/deploy
Headers: X-Upload-Secret: <secret>
Content-Type: multipart/form-data
Body: file=<zip-file>
```

### Delete Site
```http
DELETE /api/sites/:subdomain
Headers: X-API-Key: <site-api-key>
```

### List Sites
```http
GET /api/sites
Headers: X-Upload-Secret: <secret>
```

### Admin Endpoints
```http
GET /api/admin/sites          # List all sites
GET /api/admin/stats          # Storage statistics
DELETE /api/admin/sites/:id   # Delete any site
Headers: X-Admin-Secret: <admin-secret>
```

## Development

```bash
# Show all available commands
make help

# Run in development mode
make run

# Run tests
make test

# Build both binaries
make build

# Clean build artifacts
make clean
```

## Requirements

- Go 1.21+
- SQLite3

## Architecture

- **Language**: Pure Go
- **Database**: SQLite3
- **HTTP**: Standard library
- **Routing**: Host-based subdomain routing
- **Storage**: Filesystem + SQLite metadata

## Security

- Shared secret authentication for uploads
- Per-site API keys for deletion
- Separate admin secret for management
- Rate limiting (10 uploads/hour per IP)
- ZIP bomb protection
- Path traversal prevention
- Max upload size: 100MB
- Max extracted size: 500MB

## Production Deployment

### 1. Server Setup

```bash
# On your server (Ubuntu/Debian)
sudo apt update
sudo apt install -y git golang-go sqlite3

# Clone and build
git clone https://github.com/baely/staticer.git
cd staticer
make build-server
```

### 2. Configuration

```bash
# Copy and edit environment file
cp .env.example .env
nano .env
```

Set strong secrets:
```bash
UPLOAD_SECRET=$(openssl rand -hex 32)
ADMIN_SECRET=$(openssl rand -hex 32)
```

### 3. DNS Setup

Configure your DNS provider:
```
Type: A Record
Name: lab.baileys.app
Value: <your-server-ip>

Type: A Record
Name: *.lab.baileys.app
Value: <your-server-ip>
```

### 4. Systemd Service

Create `/etc/systemd/system/staticer.service`:

```ini
[Unit]
Description=Staticer Static Site Hosting
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/staticer
ExecStart=/opt/staticer/staticer-server
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable staticer
sudo systemctl start staticer
sudo systemctl status staticer
```

### 5. Reverse Proxy (Nginx)

Install Nginx:
```bash
sudo apt install -y nginx certbot python3-certbot-nginx
```

Create `/etc/nginx/sites-available/staticer`:

```nginx
server {
    server_name lab.baileys.app *.lab.baileys.app;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Increase upload size limit
        client_max_body_size 100M;
    }

    listen 80;
}
```

Enable and get SSL:
```bash
sudo ln -s /etc/nginx/sites-available/staticer /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx

# Get SSL certificate
sudo certbot --nginx -d lab.baileys.app -d "*.lab.baileys.app"
```

### 6. Firewall

```bash
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

### 7. Monitoring

View logs:
```bash
sudo journalctl -u staticer -f
```

Check disk usage:
```bash
du -sh /opt/staticer/sites
```

Monitor with admin API:
```bash
curl https://lab.baileys.app/api/admin/stats \
  -H "X-Admin-Secret: your-admin-secret"
```

## Testing

See [TESTING.md](TESTING.md) for detailed testing instructions.

Run automated tests:
```bash
make test
```

## Backup

Backup the following:
- `/opt/staticer/data/staticer.db` - Site metadata
- `/opt/staticer/sites/` - Deployed site files
- `.env` - Configuration (keep secure!)

Example backup script:
```bash
#!/bin/bash
BACKUP_DIR="/backup/staticer/$(date +%Y%m%d)"
mkdir -p $BACKUP_DIR
cp -r /opt/staticer/sites $BACKUP_DIR/
cp /opt/staticer/data/staticer.db $BACKUP_DIR/
```

## Troubleshooting

### Server won't start
- Check logs: `sudo journalctl -u staticer -n 50`
- Verify .env file exists and has correct values
- Ensure port 8080 is available: `sudo lsof -i :8080`

### Upload fails
- Check upload secret is correct
- Verify file is a valid ZIP with index.html
- Check file size is under 100MB

### Site not accessible
- Verify DNS is configured correctly: `dig lab.baileys.app`
- Check Nginx is running: `sudo systemctl status nginx`
- Test direct server: `curl -H "Host: test.lab.baileys.app" http://localhost:8080/`

### Database locked
- Only one server instance can run at a time
- Check for zombie processes: `ps aux | grep staticer`

## Contributing

Contributions welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Submit a pull request

## License

MIT
