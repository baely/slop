# parity

A RAID price optimiser. Set a usable-capacity target, put in the drive prices
you can actually get, and read off the cheapest way to build it — across drive
sizes, drive counts and layouts at once.

## How it works

Layouts are **capacity archetypes**, not brand names — RAID 5 and RAIDZ1 have
identical geometry (N−1 usable, survives any 1 drive), so they are one row
labelled with both names:

| archetype | names | usable | survives | min |
|---|---|---|---|---|
| No redundancy | RAID 0 / JBOD | N·X | nothing | 1 |
| Mirror | RAID 1 | X | N−1 drives | 2 |
| Striped mirrors | RAID 10 / mirror vdevs | N/2·X | 1 guaranteed | 4, even |
| Single parity | RAID 5 / RAIDZ1 | (N−1)·X | any 1 | 3 |
| Double parity | RAID 6 / RAIDZ2 | (N−2)·X | any 2 | 4 |
| Triple parity | RAIDZ3 | (N−3)·X | any 3 | 5 |

The optimiser enumerates every enabled layout × drive size × count up to your
bay limit, keeps what reaches the target at your required fault tolerance, and
ranks by total price or price per usable TB. The headline stat is always the
cheapest **total** — table sorting never relabels it. Per layout and size only
the best count is listed, so the table stays readable.

Numbers are geometry only. Filesystems take their own cut on top (ZFS a few
percent), and a hot spare is a bay you're not counting.

Drive prices ship as editable placeholders and persist in `localStorage`,
along with the target, bay count, tolerance, layouts and sort.

## Verification

The optimiser is a pure function tested with node against hand-derived cases:
Sunny-day (40 TB surviving 2 → 7×8TB double parity at $1,673), per-TB sort
preferring filled bays, even-only striped mirrors, cross-size wins (4×12TB
beating 6×8TB), unreachable targets returning empty, and minimum widths.

## Deployment

```sh
staticer deploy --dir site --domain parity.baileys.dev --expires never
```

Live at https://parity.baileys.dev
