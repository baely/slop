# syntax=docker/dockerfile:1.6
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/letterboxd-prep .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/letterboxd-prep /app/letterboxd-prep
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/letterboxd-prep"]
