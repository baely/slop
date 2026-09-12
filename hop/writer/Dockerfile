# syntax=docker/dockerfile:1.7
# Build from the hop module root: docker build -f writer/Dockerfile .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hop-writer ./writer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /hop-writer /hop-writer
EXPOSE 8080
ENTRYPOINT ["/hop-writer"]
