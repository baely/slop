# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum* ./
RUN go mod download

# Build statically (modernc.org/sqlite is pure Go, so CGO is not required).
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/voyage ./cmd/server

FROM gcr.io/distroless/static-debian12:latest
WORKDIR /app
COPY --from=build /out/voyage /app/voyage
EXPOSE 8080
ENV ADDR=":8080" DB_PATH="/data/voyage.db"
VOLUME ["/data"]
ENTRYPOINT ["/app/voyage"]
