# TrackUI

Live location dashboard showing current position and recent history on a map. Aggregates GPS data from Traccar, transaction data from Up Bank, and listening history from Last.fm. Includes trip detection and office presence tracking.

## Features

- Live location dot with pulsing indicator (green = recent, red = stale)
- 24-hour location trail on a Leaflet map
- Trip detection from GPS data with debouncing
- Transaction timeline from Up Bank API
- Listening history from Last.fm
- Office presence detection with ibbitot integration
- Side panel with trip, spending, and music details

## Configuration

Copy `.env.example` to `.env` and fill in your API tokens.

## Usage

```sh
pip install -r requirements.txt
python server.py
```

## Deployment

```sh
docker build --platform linux/amd64 -t registry.baileys.dev/trackui .
docker push registry.baileys.dev/trackui
docker compose up -d
```
