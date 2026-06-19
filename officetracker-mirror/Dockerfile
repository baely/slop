# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache modules first (none beyond stdlib, but keeps the layer stable).
COPY go.mod go.sum* ./
RUN go mod download

# Build a static binary (no CGO, no external deps).
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/officetracker-mirror ./cmd/server

FROM gcr.io/distroless/static-debian12:latest
WORKDIR /app
COPY --from=build /out/officetracker-mirror /app/officetracker-mirror
EXPOSE 8080
ENV ADDR=":8080" UPSTREAM="https://officetracker.com.au"
ENTRYPOINT ["/app/officetracker-mirror"]
