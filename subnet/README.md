# subnet

An IPv4 + IPv6 CIDR playground. Type a prefix, understand it instantly.

Live: <https://subnet.baileys.dev>

## What it does

- **Parses** `192.168.0.0/24`, `10.0.0.0/8`, `2406:da1c::/56`, bare addresses,
  and messy input — whitespace anywhere is stripped, a missing prefix length is
  assumed to be `/32` (IPv4) or `/128` (IPv6). Validation is live and the error
  line says exactly what is wrong, e.g.
  `Not an address: 192.168.0.300 (octet 4 is 300, above 255).`
- **IPv4 facts**: network, broadcast, first/last usable, usable host count,
  total addresses, netmask, wildcard, range, host bits, and the well-known range
  it falls in (RFC 1918, CGNAT, link-local, loopback, TEST-NET, multicast…).
- **IPv6 facts**: compressed and fully expanded network, first/last address,
  total addresses, and the number of `/64`s inside the prefix. No broadcast —
  the table says so rather than inventing one.
- **Split**: pick a longer prefix length and get the resulting subnets. The list
  is capped at 256 rows with the real count stated (`Showing 256 of 4,096.`).
  Any row's CIDR is a button that loads that subnet as the new prefix.
- **Containment**: a second input answers "is this inside?" with a yes/no line
  and the reason — the bits that match, the offset, or the address the query
  actually lands on when masked.
- **Position**: a ladder of plain `div` bars showing where the prefix sits
  inside each of its parent spaces, e.g. `192.0.0.0/8 → /16   #169 of 256`.

Presets: `192.168.0.0/24` (the default landing state), `10.0.0.0/8`,
`172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`, `169.254.0.0/16`,
`2406:da1c::/56`, `fc00::/7`, `fe80::/10`.

## How it works

Static HTML/CSS/JS, no build step, no dependencies, no network requests. All
address arithmetic is `BigInt` — IPv6 needs it (`2^128` overflows `Number`
long before it matters) and IPv4 shares the same code paths so there is one
implementation, not two.

Edge cases handled explicitly:

- A `/31` has 2 addresses and **no broadcast**. Usable hosts reads `0` by the
  classic network+broadcast rule, with a note that RFC 3021 makes both addresses
  usable on a point-to-point link.
- A `/32` is a single host: no broadcast, first == last == the address, 1 usable.
- A `/0` covers the whole space and the position ladder says so instead of
  drawing a meaningless bar.
- IPv6 prefixes longer than `/64` report `None` for `/64` subnets with the
  reason, rather than `0`.
- IPv6 compression follows RFC 5952: longest run of zero groups, leftmost on a
  tie, never eliding a single group.
- Embedded IPv4 forms parse (`::ffff:192.168.0.1` → `::ffff:c0a8:1`).
- IPv4 octets with leading zeros are rejected — `010` is ambiguously octal.

## Limits

- The split list renders at most 256 rows. The full count is always shown.
- Numbers over 15 digits display as a power of two (`2^72`); the exact digits
  are on the element's `title`.
- Containment compares prefixes only — it does not diff arbitrary ranges that
  are not CIDR-aligned.
- Last input and last containment query persist in `localStorage`.

## Deployment

```sh
staticer deploy --dir site --domain subnet.baileys.dev --expires never
```
