# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/hop .

# No libc, no shell, runs as nonroot. The sample links are baked in as a
# default; deployments mount their own file over /etc/hop/links.txt.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hop /hop
COPY links.txt /etc/hop/links.txt
EXPOSE 8080
ENV ADDR=":8080" LINKS="/etc/hop/links.txt"
ENTRYPOINT ["/hop"]
