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
  text in the **specimen log**, which persists forever.
- **Upgrades:** ballast trim (descent speed), floodlights (passive research),
  sonar array (ping/photo strength, attracts creatures), camera drones
  (auto-photography), pressure hull (depth rating).
- **Resurface (prestige).** Past 1,000 m you can end the expedition for
  **expedition grants (✦)** — a permanent multiplier on all research and
  descent speed. Depth, research and upgrades reset; the log and grants
  don't.
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
