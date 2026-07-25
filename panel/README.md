# panel

A dashboard that is rendered server-side as a **1-bit image for e-ink devices to
fetch**. A battery-powered panel cannot run a browser: it wakes up, does one
HTTP GET, draws the bytes, and goes back to sleep. This is the other end of that
wire.

The panel shows the current date and time (Australia/Melbourne, rendered large),
a configurable title line, a grid of label/value rows, a footer note, and a
small "Updated HH:MM" stamp so a stale panel is obvious at a glance.

Live at <https://panel.baileys.dev>.

## Endpoints

| method | path | auth | what |
|---|---|---|---|
| `GET` | `/healthz` | none | `200 ok` |
| `GET` | `/screen.png` | device key | the dashboard as a true 1-bit paletted PNG |
| `GET` | `/screen.bin` | device key | the same image as packed 1bpp bytes |
| `GET` | `/` | app token | preview page: the render at 1:1, byte sizes, ETags, MicroPython snippet |
| `GET` `POST` | `/edit` | app token | title, rows and footer |
| `POST` | `/key/rotate` | app token | issue a new device key |
| `GET` `POST` | `/login` | — | token login, sets a session cookie |
| `POST` | `/logout` | — | clears it |

### Query parameters (`/screen.png`, `/screen.bin`)

| param | default | notes |
|---|---|---|
| `w` | `800` | 64–2400, and `w*h` ≤ 3,000,000 |
| `h` | `480` | as above |
| `preset` | — | `800x480` (Waveshare 7.5"), `400x300` (Waveshare 4.2"), `1404x1872` (Supernote A5X) |
| `rotate` | `0` | `0`, `90`, `180`, `270` |
| `invert` | `0` | swaps black and white |
| `k` | — | the device key (or send `Authorization: Bearer <key>`) |

`w` and `h` are always the **output** dimensions. For `rotate=90` and
`rotate=270` the content is composed at `h × w` and then rotated, so the
response is exactly `w × h` whatever the rotation — which keeps
`/screen.bin` at a predictable length.

Out-of-range values return `400` with a sentence saying what was wrong, not a
blank error page.

### The PNG

Bit depth **1**, colour type **3** (paletted), a two-entry `PLTE` of `#000000`
and `#ffffff`, no `tRNS`. Not a 24-bit image that happens to contain only black
and white pixels — the encoded header says so, and a test asserts it by parsing
the chunk stream. Text is composed with anti-aliasing into an 8-bit scratch
buffer and thresholded exactly once at the end, so no grey pixel can reach the
output; another test decodes the response and scans every pixel to prove it.

### The packing (`/screen.bin`)

1 bit per pixel, **MSB first**, row-major, each row padded to a whole byte —
`framebuf.MONO_HLSB` exactly.

```
stride     = ceil(w / 8)
byte index = y * stride + x / 8
bit mask   = 0x80 >> (x % 8)
bit set(1) = WHITE pixel
length     = stride * h        (always, exactly)
```

For the default 800×480 that is `100 * 480 = 48000` bytes. The response carries
a real `Content-Length` and is never chunked, so a microcontroller can
preallocate the buffer and `readinto` it in one pass.

### Caching

Both endpoints return a strong `ETag` (SHA-256 of the exact bytes) and
`Cache-Control: private, no-cache, max-age=0, must-revalidate` — "revalidate",
not "don't store". A device that sends `If-None-Match` gets `304` with no body
while the content and the minute are unchanged, which is the difference between
a panel that lasts a month on a LiPo and one that does not.

The render is deterministic: the same content in the same minute produces
byte-identical output. That is the only thing that makes the ETag worth
anything, and there is a test for it.

## Auth

`APP_TOKEN` gates every page and every write. Send it as
`Authorization: Bearer <token>`, or log in through the form, which sets a
session cookie (`HttpOnly`, `Secure`, `SameSite=Lax`) whose value is an HMAC of
a random session id keyed by the token — never the token itself. Ten failed
checks per address per minute earns a `429`.

The screen endpoints are gated by a separate **device key**: 128 bits from
`crypto/rand`, base32 without ambiguous characters, generated on first start and
shown on the Edit page. It is a capability id, not a password, and it exists so
a panel flashed with one URL never has to carry the app token. `Rotate Key`
kills every URL already on a device.

## Configuration

| var | default | meaning |
|---|---|---|
| `ADDR` | `:8080` | listen address |
| `DATA_DIR` | `/data` | persistence directory |
| `BASE_URL` | `https://panel.baileys.dev` | used to build the MicroPython snippet |
| `APP_TOKEN` | — | **mandatory**; the process exits 1 if it is empty |
| `TZ` | — | set by the deployment; the panel clock is pinned to `Australia/Melbourne` regardless |

## Caps

| thing | cap |
|---|---|
| rows | 24 |
| title | 64 characters |
| row label / value | 48 characters each |
| footer note | 120 characters |
| request body | 32 KiB |
| render dimensions | 64–2400 per side, 3,000,000 pixels total |

Blank rows are dropped on save. Rows that do not fit the requested panel size
collapse into a `+N more` line rather than overflowing. There is no retention to
apply: the state is one small JSON document, rewritten in place.

## Storage

One file, `$DATA_DIR/panel.json`, loaded once at startup and held in memory
behind a `sync.RWMutex`. Every write goes to a temp file in the same directory,
is fsynced, renamed over the target, and the directory is fsynced too — a crash
leaves either the old file or the new one, never half of either. Mode `0600`,
because it holds the device key.

## Fonts

There is no font in the Go standard library, so this uses `golang.org/x/image`
(the one dependency): the Go typefaces for anything 12px and up, and
`basicfont.Face7x13` — a genuine 1-bit bitmap face — below that, where a
thresholded outline turns to mush on a 400×300 badge. Values are set in Go Mono
Bold, because if it is a value it is mono.

## Development

```sh
APP_TOKEN=dev-token DATA_DIR=$(mktemp -d) ADDR=:8801 go run .
open http://localhost:8801/login
```

```sh
go vet ./... && gofmt -l . && go test ./...
```

## Deployment

Runs on the host at `~/panel/docker-compose.yaml` behind Traefik at
<https://panel.baileys.dev>, built directly via docker + ssh rather than through
the infra repo.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/panel:latest --push .
```

The container runs as uid 10001, so the `/data` volume must be chowned to it.
`APP_TOKEN` comes from the host env file; the device key is generated on first
start and lives in the volume.
