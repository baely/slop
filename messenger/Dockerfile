FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /messenger .

FROM cgr.dev/chainguard/static:latest
COPY --from=build /messenger /messenger
ENTRYPOINT ["/messenger"]
