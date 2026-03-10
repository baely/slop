# Supernote Template Studio

A web app for creating custom note templates for the Supernote A5X2 Manta e-ink tablet.

## Features

- **Monthly Planner** — Calendar grid with configurable month/year and date cells
- **Weekly Planner** — Day columns with optional time slots and notes section
- **Dot Grid** — Configurable spacing and dot size
- **Lined Paper** — Ruled lines with optional margin line and header area
- **Cornell Notes** — Classic note-taking layout with adjustable cue/summary proportions
- **Isometric Grid** — Triangular dot pattern for 3D sketching

All templates export as 1920x2560px PNG files, matching the A5X2 Manta's native resolution.

## Usage

1. Select a template from the sidebar
2. Adjust configuration options
3. Click "Download PNG"
4. Transfer the PNG to your Supernote's `MyStyle` folder

## Deployment

```sh
staticer deploy --domain supernote.baileys.dev --expires never
```
