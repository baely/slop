# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/traccar-tools ./cmd/server

FROM gcr.io/distroless/static-debian12:latest
WORKDIR /app
COPY --from=build /out/traccar-tools /app/traccar-tools
EXPOSE 8080
ENV ADDR=":8080"
ENTRYPOINT ["/app/traccar-tools"]
