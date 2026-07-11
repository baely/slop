# DESCENT

An idle deep-sea expedition. Your autonomous submersible sinks continuously —
through the sunlit shallows, the twilight and midnight zones, the abyssal
plain, the hadal trenches, and eventually somewhere the charts end. The page
itself darkens as you go.

**Play it: https://descent.baileys.dev**

## How it plays

- **Descend.** Depth climbs on its own. Your **pressure hull** caps how deep
  you can go — each hull tier unlocks the next ocean zone (200 m → 1,000 m →
  4,000 m → 6,000 m → 11,000 m → ∞).
- **Research (◇)** is the currency. It trickles in passively (faster the
  deeper you are), and bursts in when you **click the water to sonar-ping**
  or **click a passing creature to photograph it**.
- **Discoveries.** 36 lifeforms — from moon jellyfish to the deepest fish
  ever recorded, and four things below the Challenger Deep floor that are not
  in any field guide. Each is logged with real(ish) natural-history flavour
  text in the **specimen log**, which persists forever. Creatures have a
  chance to appear as **rare variants** worth 6× the research.
- **Sonar contacts.** Blips surface every so often; investigate before they
  fade for creature swarms, data caches, rare sightings, debris fields — or
  nothing but a thermocline echo.
- **Wreck salvage.** Nine wrecks rest at (roughly) their historical depths —
  a ghost net, Beebe's bathysphere, the Titanic at 3,803 m, USS Johnston at
  6,456 m, the Trieste's ballast shot near the floor, and something with a
  door below it. Passing one reveals it; salvaging costs research and grants
  a permanent relic effect kept through every resurface.
- **Upgrades:** ballast trim (descent speed), floodlights (passive research),
  sonar array (ping/photo strength, attracts creatures), hydrophones (contact
  frequency), camera drones (auto-photography), pressure hull (depth rating).
- **Resurface (prestige).** Past 1,000 m you can end the expedition for
  **expedition grants (✦)**, spent in the **dry dock** on permanent refits:
  research funding, veteran pilots, a staff biologist, rare-sighting sensors,
  a reinforced keel (start at higher hull tiers) and standing refits.
- **Expedition records.** Seventeen milestones — depth firsts, photography
  counts, full-zone surveys — each a small permanent bonus.
- **Offline progress.** The vessel keeps descending while the tab is closed
  (up to 8 h); you get a surfacing report when you return.

Save state lives in `localStorage`. `?warp=N` multiplies descent speed for
testing; `DESCENT.reset()` in the console wipes the save.

## Tech

Static, no build step, no dependencies: `index.html` + `style.css` +
`creatures.js` (zone/species data) + `game.js` (engine). Canvas marine snow,
CSS-animated SVG creature silhouettes, WebAudio synth for sonar blips,
Google Fonts (Doto / Fraunces / Spline Sans Mono).

## Deployment

```
staticer deploy --domain descent.baileys.dev --expires never
```
