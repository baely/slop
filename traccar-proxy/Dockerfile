# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/traccar-proxy .

FROM gcr.io/distroless/static-debian12:latest
WORKDIR /app
COPY --from=build /out/traccar-proxy /app/traccar-proxy
EXPOSE 8080
ENV ADDR=":8080" DATA_DIR="/data"
VOLUME ["/data"]
ENTRYPOINT ["/app/traccar-proxy"]
