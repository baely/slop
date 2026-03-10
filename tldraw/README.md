# tldraw

A self-hosted instance of [tldraw](https://tldraw.com), the infinite canvas whiteboard.

## Features

- Full tldraw editor (drawing, shapes, text, arrows, etc.)
- Persistent local storage via IndexedDB
- Cross-tab sync
- No backend required

## Development

```bash
npm install
npm run dev
```

## Deployment

```bash
npm run build
staticer deploy --dir dist --domain draw.baileys.app --expires never
```
