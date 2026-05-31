# Traccar Migrate

A web tool for migrating historical location data from one [Traccar](https://www.traccar.org/)
instance to another. It **reads** from the source through the reports API and
**replays** each position into the destination through Traccar's OsmAnd
protocol — with configuration and preview steps in between, including remapping
device IDs.

## Why it works this way

Traccar's REST API can read positions (`GET /api/reports/route`) but has **no
endpoint to write raw positions** — positions only enter Traccar through a
device protocol. The standard, supported way to backfill historical data is the
**OsmAnd HTTP protocol** (default port `5055`): each point is sent as an HTTP
request keyed by the device's `uniqueId`, with the original timestamp preserved.

This means:

- The destination must have the **OsmAnd protocol enabled** (it is, by default,
  on port `5055`). The tool sends a harmless probe at connect time to confirm
  the endpoint is listening.
- Devices are matched on the destination by **`uniqueId`**. The migrator can map
  each source device to a different destination `uniqueId`, and will create any
  missing devices via the destination API before replaying.

## Steps

1. **Connect** — point at the source Traccar API, the destination Traccar API,
   and the destination OsmAnd endpoint. Authenticate with an API token
   (Traccar → Settings → preferences → tokens) or email/password. Both
   connections are validated and devices are listed.
2. **Map & range** — choose which source devices to migrate, set the
   destination `uniqueId` for each (defaults to the source's, change to remap),
   toggle auto-creation of missing devices, and pick a time window.
3. **Preview** — fetch the matching positions and review per-device counts, date
   spans, and the tracks drawn on a map. Nothing has been written yet.
4. **Migrate** — replay the positions. Progress streams live (sent / failed per
   device); cancel at any time.

Replay options: a throttle between sends (gentler on the destination) and
whether to skip positions Traccar flagged as invalid.

### Units & fields

Speed is forwarded in knots to match Traccar's internal storage; course →
bearing, plus altitude, accuracy and battery level when present. Timestamps are
sent as the original fix time so history lands at the right moment.

## Run locally

```sh
go run ./cmd/server
# open http://localhost:8080
```

`ADDR` overrides the listen address (default `:8080`). There is no database and
no persisted state — credentials live only in the browser session and are sent
straight through to the two Traccar instances.

## Deployment

Built as a single static binary in a distroless image, served behind Traefik.

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/traccar-migrate .
docker push registry.baileys.dev/traccar-migrate
docker compose up -d
```

Public URL: https://traccar-migrate.lab.baileys.dev

## Notes & caveats

- **Idempotency** — replaying the same range twice will create duplicate
  positions on the destination. Pick the time window deliberately.
- **Large ranges** — the source is read in 14-day chunks to bound memory; the
  "Everything" preset reads from 2010 to now.
- **Throughput** — every position is one HTTP round-trip to the OsmAnd port, so
  very large migrations take time. Use the throttle to avoid overwhelming a
  small destination server.
