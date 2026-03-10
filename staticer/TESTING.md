# Testing Guide for Staticer

This guide walks you through testing the Staticer platform locally.

## Prerequisites

- Go 1.21 or later
- SQLite3 (included with most systems)

## Quick Test

### 1. Build the project

```bash
make build
```

This creates two binaries:
- `staticer-server` - The web server
- `staticer` - The CLI tool

### 2. Start the server

```bash
./staticer-server
```

The server will start on `http://localhost:8080` (configured in `.env`).

### 3. Test with cURL

#### Deploy a site

```bash
# Create a test ZIP
cd test-site
zip -r ../test.zip .
cd ..

# Deploy it
curl -X POST http://localhost:8080/api/deploy \
  -H "X-Upload-Secret: test-secret-123" \
  -F "file=@test.zip"
```

Response:
```json
{
  "subdomain": "happy-tree",
  "url": "https://happy-tree.localhost",
  "api_key": "sk_...",
  "created_at": "2026-01-23T10:00:00Z",
  "file_count": 2,
  "size_bytes": 1024
}
```

#### List sites

```bash
curl http://localhost:8080/api/sites \
  -H "X-Upload-Secret: test-secret-123"
```

#### Delete a site

```bash
curl -X DELETE http://localhost:8080/api/sites/happy-tree \
  -H "X-API-Key: sk_..."
```

#### Admin endpoints

```bash
# Get stats
curl http://localhost:8080/api/admin/stats \
  -H "X-Admin-Secret: admin-secret-456"

# List all sites
curl http://localhost:8080/api/admin/sites \
  -H "X-Admin-Secret: admin-secret-456"

# Delete any site
curl -X DELETE http://localhost:8080/api/admin/sites/happy-tree \
  -H "X-Admin-Secret: admin-secret-456"
```

### 4. Test with CLI

```bash
# Configure CLI
./staticer config --secret test-secret-123 --server http://localhost:8080

# Deploy test site
./staticer deploy --dir test-site

# List sites
./staticer list

# Delete site
./staticer delete happy-tree
```

### 5. Test with Web Dashboard

1. Open browser to `http://localhost:8080`
2. Enter upload secret: `test-secret-123`
3. Click "Save"
4. Drag and drop `test.zip` or use the file picker
5. See your deployed site in the list

## Testing Subdomain Routing

Since we're testing locally on `localhost`, subdomain routing won't work without additional setup. For production testing with real subdomains:

### Option 1: Edit /etc/hosts

Add entries to `/etc/hosts`:
```
127.0.0.1  lab.baileys.app
127.0.0.1  happy-tree.lab.baileys.app
127.0.0.1  blue-sky.lab.baileys.app
```

Then update `.env`:
```bash
SERVER_HOST=lab.baileys.app
```

Restart server and access `http://lab.baileys.app:8080`

### Option 2: Use a tunneling service

Use ngrok, localtunnel, or similar:
```bash
ngrok http 8080
```

Update `.env` with the ngrok URL:
```bash
SERVER_HOST=your-id.ngrok.io
```

## Automated Tests

Run the test suite:

```bash
make test
```

## Manual Test Checklist

- [ ] Server starts successfully
- [ ] Dashboard loads at main domain
- [ ] File upload works (drag & drop and file picker)
- [ ] Site deploys and returns URL
- [ ] Deployed site is accessible and serves files correctly
- [ ] Static assets (CSS, JS, images) load properly
- [ ] Delete works with correct API key
- [ ] Delete fails with wrong API key
- [ ] Upload requires valid secret
- [ ] Rate limiting works (10 uploads/hour)
- [ ] Large file rejection (>100MB)
- [ ] ZIP bomb protection works
- [ ] Path traversal blocked (../ in ZIP)
- [ ] Admin endpoints require admin secret
- [ ] CLI config saves credentials
- [ ] CLI deploy creates and uploads ZIP
- [ ] CLI list shows deployed sites
- [ ] CLI delete removes site

## Security Tests

### Test authentication

```bash
# Should fail without secret
curl -X POST http://localhost:8080/api/deploy \
  -F "file=@test.zip"

# Should fail with wrong secret
curl -X POST http://localhost:8080/api/deploy \
  -H "X-Upload-Secret: wrong" \
  -F "file=@test.zip"
```

### Test ZIP bomb protection

Create a large uncompressed file:
```bash
dd if=/dev/zero of=large.txt bs=1M count=600
zip test-large.zip large.txt
```

Upload should be rejected if extracted size > 500MB.

### Test path traversal

Create a ZIP with `../` in paths - should be rejected.

## Performance Testing

Test concurrent uploads:

```bash
# Install hey: go install github.com/rakyll/hey@latest

# 100 requests, 10 concurrent
hey -n 100 -c 10 -m POST \
  -H "X-Upload-Secret: test-secret-123" \
  -F "file=@test.zip" \
  http://localhost:8080/api/deploy
```

## Cleanup

```bash
# Stop server (Ctrl+C)

# Clean up test data
make clean

# Or manually:
rm -rf sites/*
rm data/staticer.db*
```

## Common Issues

### Database locked

If you see "database is locked" errors:
- Only one server instance can run at a time
- Make sure previous server stopped cleanly

### Permission denied on sites directory

```bash
chmod 755 sites
```

### Port already in use

Change `SERVER_PORT` in `.env` to a different port.

## Next Steps

For production deployment, see the main README.md for:
- DNS configuration
- TLS setup
- Systemd service
- Reverse proxy configuration
