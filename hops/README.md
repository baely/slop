# hops

Paste raw `traceroute`, `mtr` or `tracert` output; get the path as a readable
table instead of a wall of text. Per-hop latency bars, packet loss, address
scope, and a few derived lines about what actually happened.

Live: https://hops.baileys.dev

## What it does

- **Detects the format** on paste and says which one it thinks it is:
  `traceroute (BSD/macOS)`, `traceroute (Linux)`, `traceroute (Unix)` when the
  header is missing, `mtr --report`, or `tracert (Windows)`. Detection scores
  every line rather than trusting a header, so a pasted fragment still works.
- **One row per hop**: number, hostname, address, every individual probe time,
  best / average / worst, and loss.
- **Latency bar** per hop, scaled to the slowest hop in the trace, so the jump
  where the packet leaves the LAN is visible without reading any numbers.
- **Address scope** in words for every hop — Private (RFC1918), CGNAT
  (RFC6598), Link-local, Unique local (ULA), Loopback, Multicast, Reserved or
  Public — which is how you see where your own network stops.
- **Derived callouts**: total hops and how many were silent, the largest jump
  between consecutive responding hops, which hops lost packets, and the first
  public address in the path. All computed from the paste, nothing hardcoded.

Four sample traces are built in (`Load Example`, `IPv6 Trace`, `mtr Report`,
`Windows tracert`). On a first visit the example loads so the page isn't blank;
after that the textarea contents persist in `localStorage`.

## Formats

| Format | Shape |
|---|---|
| macOS/BSD `traceroute` | ` 1  router.lan (192.168.0.1)  1.234 ms  1.111 ms  1.222 ms` |
| Linux `traceroute` | same, plus `!H` / `!N` / `!X` annotations |
| `mtr --report` | `  1.\|-- router.lan   0.0%  10  1.1  1.3  1.0  2.4  0.4` (and `-b`'s `name (addr)`) |
| Windows `tracert` | `  1     1 ms     1 ms     1 ms  router.lan [192.168.0.1]` |

IPv6 is handled throughout, including hops that print only an address, zone
suffixes (`fe80::1%eth0`), and IPv4-mapped addresses (`::ffff:10.0.0.1`
classifies as the v4 address it wraps).

Also handled: hops whose probes come back from different routers, `traceroute
-n` output with no hostnames, `traceroute -A` origin-AS tags (`[AS1221]`),
trailing MPLS label lines, CRLF line endings, and hop lines with no header
above them.

## Notable behaviour and limits

- **Timeouts are a hop, not a gap.** `* * *`, `Request timed out.` and mtr's
  `???` all render as `No reply` with the word visible. Missing probes count
  toward loss and are excluded from best/average/worst, so a hop that answered
  twice out of three averages over the two answers — never over three with a
  zero mixed in.
- mtr prints `0.0` across every timing column for a hop at 100% loss. That is
  absence, not a fast hop, so those zeros are discarded.
- `tracert` reports anything sub-millisecond as `<1 ms`. Those probes are shown
  verbatim and scored as **0.5 ms** — the midpoint of the bucket Windows
  actually reported. Averages for such hops are approximate by definition.
- BSD and Linux `traceroute` produce near-identical hop lines. The flavour is
  guessed from the header (`52`/`12` byte packets and `64 hops max` read as
  BSD/macOS; `60`/`80` and `30 hops max` as Linux) or a `_gateway` hostname.
  With no header it reports `traceroute (Unix)` rather than guessing.
- The largest jump compares each responding hop with the previous *responding*
  hop, so a timeout in the middle doesn't hide a jump on either side of it.
- Scope is decided from the address alone. A hop that reports only a hostname
  (plain `mtr --report`) gets no scope tag rather than a misleading one.
- Everything runs in the page: no lookups, no reverse DNS, no geolocation, no
  network requests of any kind. The paste stays in `localStorage`.

## Verification

The parser lives in `site/app.js` between the `parser:start` / `parser:end`
markers as pure, DOM-free functions. The scratch test harness slices that block
out of the shipped file and runs it under node, so the tested code is the
deployed code rather than a copy — 177 assertions across all four formats, an
IPv6 trace, tracert-over-IPv6, address classification boundaries, and ugly
input (empty, prose, header-only, all-timeout, CRLF).

## Deployment

Static; no build step.

```sh
staticer deploy --dir site --domain hops.baileys.dev --expires never
```
