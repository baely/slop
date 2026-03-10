# messenger

HTTP service that forwards messages to Chatwoot as incoming conversations.

## Configuration

Copy `.env.example` to `.env` and fill in your Chatwoot details.

## Usage

```
curl -X POST http://localhost:8080/ \
  -H "Content-Type: application/json" \
  -d '{"name": "alice", "message": "hey"}'
```

## Build

```
make build          # local binary
make docker-build   # docker image
make docker-push    # push to registry
```
