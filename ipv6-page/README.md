# ipv6-page

A tiny, text-only website served by nginx in a Docker container. It shows a single
message thanking visitors for reaching the host over IPv6 and lists the addresses
the page answers on.

```
thank you for visiting my ipv6 address. you can reach this page on the
following addresses:

  2401:2520:243c::82:443
  2401:2520:243c::82:80
  2401:2520:243c::8008:35
  2401:2520:243c::1337
  2401:2520:243c:6969:6969:6969:6969
```

The addresses are static text — nginx doesn't detect them. Edit `index.html` to
change what's listed.

## Run it

```sh
docker compose up -d --build
```

Then open <http://localhost:8080>.

Or without compose:

```sh
docker build -t ipv6-page .
docker run -d -p 8080:80 --name ipv6-page ipv6-page
```

## Serving on your actual IPv6 addresses

The point of the page is to be reachable over IPv6, so nginx has to bind to those
addresses on the host. nginx inside the container already listens on both `80` and
`[::]:80`. The rest is host networking.

The simplest option on Linux is host networking, which puts the container directly
on the host's network stack so it answers on every address the host holds —
including addresses from your delegated prefix:

```sh
docker run -d --network host --name ipv6-page ipv6-page
```

With host networking the container listens on port 80 for every host address; the
`-p` port mapping is ignored. Make sure the host has the relevant addresses
assigned and that your router/firewall forwards the traffic.

## Files

- `index.html` — the entire page
- `nginx.conf` — server block, listens on IPv4 and IPv6 port 80
- `Dockerfile` — `nginx:alpine` + the two files above
- `docker-compose.yml` — one-command local run
