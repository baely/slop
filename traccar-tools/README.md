# Traccar Tools

A small suite of browser-based utilities for working with [Traccar](https://www.traccar.org/)
instances, served as one site. Connections are made straight from your browser
session to the Traccar servers you name — there is no database and nothing is
persisted server-side.

Two tools:

## Migrate (`/migrate`)

Replay historical positions from one Traccar instance into another.

Traccar's REST API can read positions (`GET /api/reports/route`) but has no
endpoint to write raw positions — they only enter through a device protocol. So
the source is read via the reports API and each point is replayed into the
destination through the **OsmAnd protocol** (default port 5055), keyed by device
`uniqueId` with the original timestamp preserved.

- **Connect** the source API, destination API and destination OsmAnd endpoint
  (the OsmAnd reachability is probed harmlessly — no data written).
- **Map** each source device to a destination `uniqueId` (defaults to the
  source's; change it to remap), with optional auto-creation of missing devices.
- **Preview** per-device counts, date spans and tracks on a map.
- **Migrate** with live streaming progress; cancellable mid-run.

Note: replaying the same range twice creates duplicate positions — pick the
window deliberately.

## Export (`/export`)

Export full-fidelity tracks for a device and time range, in your choice of
format. Unlike Traccar's built-in bare-coordinate export, every point keeps its
timestamp, elevation, speed, course, accuracy and **all** device attributes.

| Format | What it carries |
| --- | --- |
| **GPX** 1.1 | `<ele>`/`<time>` plus speed, course, accuracy and every attribute under a `traccar:` extension namespace. Widely supported. |
| **KML** | `gx:Track` — time-aware and animatable in Google Earth; per-point speed/course/accuracy and attributes as `gx:SimpleArrayData`. |
| **GeoJSON** | `FeatureCollection`, one `LineString` per segment with `coordTimes` and a full per-point `properties.points` array. Best for code / web maps. |

Speed is given in both m/s and Traccar's native knots; times are UTC. A
configurable gap threshold starts a new segment (track/trkseg/feature) when
consecutive fixes are far apart in time, so separate trips aren't joined by a
straight line.

## Run locally

```sh
go run ./cmd/server
# open http://localhost:8080
```

`ADDR` overrides the listen address (default `:8080`).

## Deployment

Single static binary in a distroless image, served behind Traefik.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/traccar-tools .
docker push registry.baileys.dev/traccar-tools
docker compose up -d
```

Public URL: https://traccar-tools.lab.baileys.dev

## Notes

- The source is read in 14-day chunks to bound memory; the "Everything" preset
  reads from 2010 to now.
- Large windows produce large exports / long migrations — every fix is one row.
  Narrow the range if you only need part of the history.
