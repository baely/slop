# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache modules first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/shoebox ./cmd/shoebox

# Runtime needs chromium: every email body is snapshotted to an email.pdf
# attachment. That makes this alpine rather than distroless.
FROM alpine:3.20
RUN apk add --no-cache chromium font-noto ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/shoebox /app/shoebox
EXPOSE 8080 2525
ENV ADDR=":8080" SMTP_ADDR=":2525" DATA_DIR="/data"
VOLUME ["/data"]
ENTRYPOINT ["/app/shoebox"]
