# me

A directory of twelve aesthetic variations on a personal landing page for Bailey Butler.

Each variation links to the same five destinations (GitHub, LinkedIn, Instagram, Letterboxd, blog) but commits to a completely different visual direction.

## Variations

1. **Swiss Minimal** — sharp grotesque, 12-column grid, almost nothing
2. **Brutalist Block** — yellow + black, raw concrete, Bowlby One
3. **Terminal** — dark CLI window, JetBrains Mono, prompt + cursor
4. **Editorial Serif** — Fraunces, drop caps, magazine layout
5. **Y2K Chrome** — holographic gradients, beveled glass, Major Mono
6. **Notebook Pages** — ruled paper, Caveat handwriting, doodles
7. **Newspaper Print** — broadsheet masthead, blackletter, multi-column
8. **Risograph Print** — two-color print, pink + blue, paper grain
9. **CRT Glow** — scanlines, neon chromatic aberration, Press Start 2P
10. **Bento Grid** — modular cards, Instrument Serif, gradient tiles
11. **Star Chart** — dark night sky, links as named stars
12. **Postage Stamps** — kraft envelope, tilted stamps, postmark

## Structure

- `index.html` — directory page that links to each variation
- `variations/01-swiss.html` through `12-stamp.html` — each design

All links are placeholder `#` — swap in real URLs before sharing.

## Deployment

Deployed as a static site via staticer:

```sh
staticer deploy
```

To make it permanent at a subdomain:

```sh
staticer deploy --domain me.baileys.dev --expires never
```
