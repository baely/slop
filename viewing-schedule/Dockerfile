# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum* ./
RUN go mod download

# Build statically (modernc.org/sqlite is pure Go, so CGO is not required).
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/viewing-schedule ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/viewing-schedule /app/viewing-schedule
USER nonroot:nonroot
EXPOSE 8080
ENV ADDR=":8080" DB_PATH="/data/viewing.db"
VOLUME ["/data"]
ENTRYPOINT ["/app/viewing-schedule"]
