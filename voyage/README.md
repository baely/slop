# Voyage

A holiday planning app for weighing up trip options and letting fellow
travellers vote. Single Go binary, SQLite-backed, server-rendered HTML.

This first iteration covers the **Ideate** and **Plan** stages of the planning
lifecycle (Ideate → Plan → Book → Anticipate → Travel).

## What it does

A **trip** has a title and one or more **locations**, and is planned through two
modes:

- **Ideate** — brainstorm freely. Add **budget** options, and under each budget
  the **hotel** options you'd consider. List candidate **date ranges**, and build
  an **activity wishlist**.
- **Plan** — narrow down. Review what the group has voted for, mark a
  **preferred / selected** stay per budget, share the link, and add/rank options
  without leaving the page.

### Cost totals

Set the trip's **party size** and the app turns the shorthand figures into
estimated totals: a per‑person budget becomes a group total (× people), and a
per‑night stay becomes a stay total (× nights, derived from the most‑voted date
range). Nights are shown on each date range.

### Share, vote & rank

Every trip has an unguessable **share link**. Fellow travellers open it (no
sign-in), enter a name, and weigh in:

- **Budgets, date ranges and accommodation** are **upvoted** (one vote each); the
  shared view and Plan rank them by total votes.
- **Activities** are **ranked**: each traveller orders them by preference, and
  the app aggregates everyone's rankings (Borda points) into a **weighted final
  ranking** — the order shown on the Plan screen, with each activity's score.

(Discussion happens in person — there's intentionally no free-text commenting.)

## Designed to generalise

The data model is deliberately generic so future features slot in without schema
churn:

- **Axes → options → combos → combo items.** Budgets and date ranges are options
  on two axes; hotels are `combo_item`s grouped under a budget-only combo. Combos
  are N-axis tuples and items carry a `category`, so grouping by more dimensions
  or adding flights/transport/restaurants is a small step.
- **Lists → list items.** Activities is one `activity` list; more lists
  (restaurants, packing) reuse the same tables.
- **Votes & comments** key off a generic `(target_type, target_id)` pair
  (`axis_option`, `combo_item`, `list_item`), so anything can become votable.
- The trip `stage` column already spans all five lifecycle stages.

## Run locally

```bash
ADMIN_TOKEN=secret go run ./cmd/server
# visit http://localhost:8080  (sign in with the ADMIN_TOKEN)
```

The DB is created at `DB_PATH` (default `/data/voyage.db`; set it to `./voyage.db`
for local runs).

## Configuration

All via environment variables (see `.env.example`):

| Var           | Default                        | Purpose                                        |
| ------------- | ------------------------------ | ---------------------------------------------- |
| `ADDR`        | `:8080`                        | Listen address                                 |
| `DB_PATH`     | `/data/voyage.db`              | SQLite file path                               |
| `TITLE`       | `Voyage`                       | UI title                                       |
| `CURRENCY`    | `AUD`                          | Default currency code prefilled in forms       |
| `ADMIN_TOKEN` | _(empty = open, dev only)_     | Planner access token (required in production)  |
| `BASE_URL`    | _(empty = derive from request)_| Base for absolute share links                  |
| `TRUST_PROXY` | `0`                            | Trust `X-Forwarded-Proto` behind a proxy       |

## Project layout

```
cmd/server/          entry point (config + lifecycle)
internal/store/      SQLite store; one file per spine (trips, axes, combos, lists, voting)
internal/server/     HTTP router, owner handlers, auth, share handlers, templates
```

## Tests

```bash
go test ./...
```

## Deploy (Docker + Traefik)

```bash
cp .env.example .env   # set ADMIN_TOKEN (and BASE_URL)
docker compose up -d --build
```

The image is built for `linux/amd64` and published to `registry.baileys.dev`:

```bash
docker build --platform linux/amd64 -t registry.baileys.dev/voyage:latest .
docker push registry.baileys.dev/voyage:latest
```

Traefik routes `voyage.baileys.app` to the service on port `8080`; the SQLite DB
persists in the `voyage-data` volume.
