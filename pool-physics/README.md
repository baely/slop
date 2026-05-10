# Pool Physics

A pocketless billiards sandbox in pure HTML/Canvas/JS. Place balls of varying mass, fling them with a click‑and‑drag, and watch momentum transfer through elastic collisions, bounce off the walls, and decay through friction.

## Features

- **Place & fling** — click empty felt to drop a ball, drag from any ball to set its launch velocity. A live aim line and arrowhead show direction and magnitude in real time.
- **Mass‑aware physics** — heavier balls are bigger and move less when struck, exactly as the impulse equations dictate. Conservation of momentum is preserved across collisions.
- **Wall bounces** — angle of incidence equals angle of reflection, with a small restitution loss per bounce.
- **Tunable elasticity** — slider controls the coefficient of restitution for ball‑on‑ball collisions, from perfectly elastic (1.00) down to highly inelastic (0.50).
- **Optional rolling friction** — toggle on for realistic decay, off for endless billiard physics.
- **Live readouts** — kinetic energy and total |momentum| update each frame.
- **Rack the table** — preset 15‑ball triangle plus cue ball, ready to break.
- **Vector overlay** — show velocity arrows on every moving ball.
- **Shift‑click** — remove a ball.

## Run locally

It's just three files, no build step.

```bash
python3 -m http.server 8000
# then open http://localhost:8000
```

## Deployment

Static site, deployed via [staticer](../staticer):

```bash
staticer deploy --domain pool.baileys.dev --expires never
```

## Files

- `index.html` — markup and HUD shell
- `style.css` — felt aesthetic, controls, typography
- `script.js` — physics engine, rendering, interaction
