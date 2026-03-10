# Listing

Dynamic service index page that auto-discovers running Docker containers. Scans for containers with `baileys.public.*` labels and renders them as a card grid. Listens to Docker events to update in real-time as containers start and stop.

## Labels

Add these labels to your Docker containers to have them appear on the index page:

- `baileys.public.url` (required) — the public URL of the service
- `baileys.public.title` — display name
- `baileys.public.description` — short description

## Usage

Requires access to the Docker socket.

```sh
make build
./listing
```

## Deployment

```sh
make docker-build
make docker-push
docker compose up -d
```
