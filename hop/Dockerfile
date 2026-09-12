# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /hop .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /hop /hop
EXPOSE 8080
ENTRYPOINT ["/hop", "-links", "/links.txt"]
