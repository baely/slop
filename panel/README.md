# panel

A **block-based layout editor for e-ink screens**, rendered server-side as a
1-bit image for a device to fetch. A battery-powered panel cannot run a
browser: it wakes up, does one HTTP GET, draws the bytes, and goes back to
sleep. This is the other end of that wire.

A screen is a canvas of positioned blocks. You add them, drag them, size them,
order them and delete them; the layout is the thing you control, not the
contents of somebody else's template.

Live at <https://panel.baileys.dev>.

## Blocks

Every block has `x`, `y`, `w`, `h` in pixels plus a 9-way `anchor` for how its
content sits inside the box. They draw in list order, so a later block covers
an earlier one. Nothing a block draws can escape its own box — the clip is
enforced by the canvas view, not by each drawing routine remembering.

| type | what it is |
|---|---|
| `text` | arbitrary content, multi-line, optional wrapping, optional inversion (white on black) |
| `clock` | live time — 24h, 24h with seconds, 12h, 12h with seconds, 12h without the suffix |
| `date` | live date — long, medium, short, ISO, weekday, day-and-month |
| `rows` | the label/value table, now one block type among many |
| `list` | plain lines, optional bullets |
| `divider` | horizontal or vertical rule, thickness in px |
| `box` | rectangle, outlined or filled. A filled box is how you get a black header bar |
| `progress` | a labelled bar with a 0–100 value |
| `image` | an uploaded picture, auto-dithered to 1-bit, scaled to fit |
| `data` | a value fetched from a JSON URL server-side, on an interval |

### `image`

Uploads are decoded (PNG, JPEG or GIF), converted to greyscale and downscaled
to 1600px on the long side. The **dither is per block, at render time** —
Floyd–Steinberg, Atkinson or a hard threshold — so you can change the look of a
scan without re-uploading it. Atkinson keeps contrast and open highlights,
which usually suits a film scan on e-ink; Floyd–Steinberg keeps gradients;
threshold is for line art. `fit` is `contain`, `cover` or `stretch`.

### `data`

Fetches JSON from a URL, extracts a value by dotted path (`current.temp_c`,
array indices allowed: `list.0.name`), and renders it through a small text
template (`{{v}}°C`).

**A render never waits on the network.** A background refresher goes out on
each block's interval; the renderer only ever reads the cache. A slow endpoint
costs you a stale reading, not a frame, and a stale value is drawn with a
trailing `·` rather than passed off as current. A block that has never
succeeded shows its `fallback` text and retries every 60 seconds regardless of
its configured interval, so a URL you are typing into the editor starts working
promptly.

The URL is fetched **by this server**, so it is fenced:

- `http` and `https` only, ports 80 and 443 only.
- The check runs on the **resolved address**, inside the dialer's `Control`
  hook, on every connection including redirects — so a hostname that resolves
  to a private address, or re-resolves to one on the second request, is still
  refused. Loopback, RFC1918, link-local (including `169.254.169.254`),
  carrier-grade NAT, multicast and IPv4-mapped equivalents are all blocked.
- Response body capped at 512 KiB, at most 3 redirects, 1–15 second timeout.

## Type on a 1-bit panel

The output has no antialiasing, so **font and size are not independent choices**
and the app refuses to pretend they are. The rules live in the data
(`fonts.go`), which is why the validator, the editor and the renderer cannot
disagree.

| face | kind | legal sizes |
|---|---|---|
| Pixel 7×13 (`basicfont.Face7x13`) | bitmap | 13, 26, 39, 52 |
| Pixel 8×16 (Inconsolata) | bitmap | 16, 32, 48, 64 |
| Pixel 8×16 Bold (Inconsolata) | bitmap | 16, 32, 48, 64 |
| Go Regular | outline | 16–400 |
| Go Bold | outline | 14–400 |
| Go Mono | outline | 16–400 |
| Go Mono Bold | outline | 16–400 |

A **bitmap face** is legal only at its native size and whole multiples of it.
It is drawn by rendering the string once at 1×, thresholding the mask, then
replicating each pixel `scale × scale` — nearest neighbour, no resampling
filter anywhere near a glyph. A test asserts that a 2× render is *exactly* the
1× render with every pixel doubled.

An **outline face** is rasterised with `font.HintingFull` and thresholded. Each
one carries the minimum size it actually survives, which was **measured, not
guessed**: every glyph of Go Regular / Bold / Mono was rendered at 10–26px,
thresholded, and checked for enclosed counters surviving in `a e o g p b d q 0
6 8 9 B D O P R` and for total ink area against true coverage. Counters hold
from 16px for Regular and Mono, 14px for Bold. Below that the line turns to
porridge and the app says so, by name:

```
Go Regular at 11px thins out on e-ink. Try Pixel 8×16 at 16px.
Pixel 8×16 has no 20px size: a bitmap face only scales in whole multiples of 16px. Use 16, 32, 48 or 64px.
```

Picking a font reconfigures the size control — a picker of legal sizes for
bitmap faces, a bounded number field for outline faces — and a **live sample
strip** shows the actual rendered PNG at 1:1 (never CSS-scaled, which would
resample the very thing you are judging). Above 72px the strip caps and says so.

### The alpha cutoff

Thresholding an antialiased glyph with a naive 50% cut eats thin stems. The
cutoff here is **116** of 255 coverage, chosen from the same measurement:

- 96 keeps every counter down to 12px but lays down 20–30% more ink than the
  glyph covers — everything comes out fattened.
- 128 tracks true area well but starts eating counters below 16px.
- 116 holds area retention at 1.00–1.12 across the legal range and keeps 17/17
  counters from 15px up. Erring a hair toward keeping ink is the right
  direction: a stem you thresholded away is gone; a stem one pixel too fat is
  merely a bit bold.

The greyscale threshold is *derived* from it (`255 - alphaCutoff + 1`), not
chosen separately, and a test fails if the two drift apart.

## The editor

- A live preview of the actual rendered PNG at 1:1, with every block outlined
  over it.
- **Drag a block to move it, drag its corner handle to resize.** Snaps to 8px;
  hold Shift for free placement. Mouse and touch (pointer events). The numeric
  x/y/w/h fields stay in sync both ways.
- Add / duplicate / delete / move up / move down.
- Per-block fields, shown only for that block's type.
- **Edit Layout JSON** — the whole document in a textarea, validated on save,
  with the offending block and field named.
- Three starter layouts, one click each: Clock And Rows, Photo Frame, Status
  Board.

**Without JavaScript everything still works.** The numeric fields and the form
buttons are the interface; the drag overlay is an enhancement. The browser
resubmits every block on every button press, so add/move/delete never eats the
blocks you were editing, and a rejected save comes back as a 400 with your work
still in the form.

### Validation

Every failure names the block, its type and the field. There is no generic
path:

```
block 3 (box): "w" must be at least 1 pixel, got -40
block 4 (box): "x" 700 plus "w" 200 is 900, past the right edge of a 800px wide screen
block 2: unknown block type "sparkline". known types: text, clock, date, rows, list, divider, box, progress, image, data
block 6 (image): "image" is required: upload an image and pick it
block 1 (data): "path" is required: the dotted path to the value, e.g. current.temp_c
```

## Screens

Named screens, each with its own dimensions, rotation, invert, blocks and
**device key**. Add, duplicate, rename and delete.

`Screen.w`/`Screen.h` are the **composition** size — the canvas you lay blocks
out on. Rotation is applied afterwards, so a 90° or 270° screen is delivered
`h × w`. (Under the previous version `w`/`h` were the *output* size. With the
default rotation of `0` the two readings are identical, which is every screen
that exists in practice; only a rotated screen sees a difference.)

## Endpoints

| method | path | auth | what |
|---|---|---|---|
| `GET` | `/healthz` | none | `200 ok` |
| `GET` | `/s/{name}/screen.png` | device key | that screen as a true 1-bit paletted PNG |
| `GET` | `/s/{name}/screen.bin` | device key | the same image as packed 1bpp bytes |
| `GET` | `/screen.png` `/screen.bin` | device key | **the first screen**, unchanged from before, so anything already flashed onto a device keeps working |
| `GET` | `/` | app token | preview page: the render at 1:1, byte sizes, ETags, MicroPython snippet. `?s=name` picks the screen |
| `GET` | `/edit` | app token | the block editor. `?s=name`, `?b=index` |
| `POST` | `/edit` | app token | save the screen; `op=save\|add\|dup.N\|del.N\|up.N\|down.N` |
| `POST` | `/screens` | app token | `op=add\|duplicate\|delete` |
| `POST` | `/starter` | app token | load a starter layout |
| `GET` | `/layout.json` | app token | the whole document as JSON |
| `POST` | `/layout` | app token | replace the whole document |
| `POST` | `/images` | app token | multipart upload |
| `POST` | `/images/delete` | app token | remove one |
| `GET` | `/images/{id}/pic.png` | app token | the stored greyscale, for editor thumbnails |
| `GET` | `/sample.png?font=&size=` | app token | the font sample strip |
| `POST` | `/key/rotate` | app token | issue a new device key for one screen |
| `GET` `POST` | `/login` | — | token login, sets a session cookie |
| `POST` | `/logout` | — | clears it |

### Query parameters (`screen.png`, `screen.bin`)

| param | default | notes |
|---|---|---|
| `w` `h` | the screen's own | 64–2400, `w*h` ≤ 3,000,000. Blocks keep their pixel coordinates, so a different size crops rather than reflows |
| `preset` | — | `800x480` (Waveshare 7.5"), `400x300` (Waveshare 4.2"), `1404x1872` (Supernote A5X) |
| `rotate` | the screen's own | `0`, `90`, `180`, `270` |
| `invert` | the screen's own | swaps black and white |
| `k` | — | the device key (or send `Authorization: Bearer <key>`) |

Out-of-range values return `400` with a sentence saying what was wrong.

### The PNG

Bit depth **1**, colour type **3** (paletted), a two-entry `PLTE` of `#000000`
and `#ffffff`, no `tRNS`. Not a 24-bit image that happens to contain only black
and white pixels — the encoded header says so, and a test asserts it by parsing
the chunk stream. Everything is composed into an 8-bit greyscale scratch buffer
and thresholded exactly once at the end, in one function; another test decodes
the response and scans every pixel.

### The packing (`screen.bin`)

1 bit per pixel, **MSB first**, row-major, each row padded to a whole byte —
`framebuf.MONO_HLSB` exactly.

```
stride     = ceil(w / 8)
byte index = y * stride + x / 8
bit mask   = 0x80 >> (x % 8)
bit set(1) = WHITE pixel
length     = stride * h        (always, exactly)
```

For 800×480 that is `100 * 480 = 48000` bytes. The response carries a real
`Content-Length` and is never chunked, so a microcontroller can preallocate the
buffer and `readinto` it in one pass.

### Caching

Both endpoints return a strong `ETag` (SHA-256 of the exact bytes) and
`Cache-Control: private, no-cache, max-age=0, must-revalidate` — "revalidate",
not "don't store". A device that sends `If-None-Match` gets `304` with no body
while the layout, the minute and the data values are all unchanged.

The render is deterministic, and the cache key covers the layout version, the
render params, the clock bucket and a hash of the resolved data values — so a
changed reading breaks the ETag, and an unchanged one does not. A screen with a
**seconds** clock format re-renders every second instead of every minute, which
a battery panel will feel; the editor says so next to the format picker.

## Auth

`APP_TOKEN` gates every page and every write. Send it as
`Authorization: Bearer <token>`, or log in through the form, which sets a
session cookie (`HttpOnly`, `Secure`, `SameSite=Lax`) whose value is an HMAC of
a random session id keyed by the token — never the token itself. Ten failed
checks per address per minute earns a `429`.

Each screen is gated by its own **device key**: 128 bits from `crypto/rand`,
base32 without ambiguous characters. It is a capability id, not a password, and
it exists so a panel flashed with one URL never has to carry the app token.
Rotating one screen's key does not touch another's.

### CSRF

Every state-changing route consults **`Sec-Fetch-Site` first** and trusts it
absolutely: browsers set it, page script cannot, and a cross-site page has no
way to forge it. Only when it is absent (an old browser, or curl) does the
check fall back to `Origin` and then `Referer`.

`Referrer-Policy` is **`same-origin`, deliberately not `no-referrer`**: under
`no-referrer` Chrome sends `Origin: null` on same-origin form posts, so an
Origin-first check rejects the app's own forms. There is a test pinning both
the header and the ordering.

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
| screens | 12 |
| blocks per screen | 48 |
| `data` blocks per screen | 8 |
| rows in a `rows` block | 24 |
| lines in a `list` block | 24 |
| text block content | 480 characters |
| labels / values | 48 characters |
| images stored | 20, 4 MiB each, 1600px long side |
| form body | 512 KiB |
| render dimensions | 64–2400 per side, 3,000,000 pixels total |
| data response body | 512 KiB, 3 redirects, 15s timeout |

Content that does not fit its block is truncated with an ellipsis; rows and
lists that overflow collapse into a `+N more` line rather than spilling out.

## Storage

One file, `$DATA_DIR/panel.json`, plus `$DATA_DIR/images/`. Loaded once at
startup and held in memory behind a `sync.RWMutex`. Every write goes to a temp
file in the same directory, is fsynced, renamed over the target, and the
directory is fsynced too — a crash leaves either the old file or the new one,
never half of either. Mode `0600`, because it holds the device keys.

**The old single-layout document is migrated on first start**: title, rows and
footer become a screen whose blocks reproduce the old fixed arrangement, and
the existing device key is carried across, so a panel already on the wall keeps
working and can then be pulled apart. Pasting layout JSON also matches device
keys back by screen name, so the escape hatch cannot silently kill a flashed
URL.

## Dependencies

Go standard library, plus `golang.org/x/image` — for the Go typefaces,
Inconsolata's bitmap faces, `basicfont`, `opentype` and `draw.CatmullRom` for
image scaling. There is no font in the standard library, which is the whole
reason for the one exception.

## Development

```sh
APP_TOKEN=dev-token DATA_DIR=$(mktemp -d) ADDR=:8801 go run .
open http://localhost:8801/login
```

```sh
go vet ./... && gofmt -l . && go test -race ./...
```

## Deployment

Runs on the host at `~/panel/docker-compose.yaml` behind Traefik at
<https://panel.baileys.dev>, built directly via docker + ssh rather than
through the infra repo.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/panel:latest --push .
```

The container runs as uid 10001, so the `/data` volume must be chowned to it.
`APP_TOKEN` comes from the host env file; device keys are generated on first
start and live in the volume alongside the uploaded images.
