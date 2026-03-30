# Ranker

A pairwise comparison ranking tool. Add items, compare them head-to-head, and get a sorted ranking using a merge sort algorithm driven by your choices.

## Features

- **Step 1**: Name your ranking and add items (single or bulk, newline-separated)
- **Step 2**: Choose between pairs of items until a full ranking is determined
- **Undo**: Step back your last pick during comparisons (button or Ctrl+Z)
- **Resume**: In-progress rankings persist across page refreshes
- **Results**: View your final ranked list with comparison count
- **History**: Past rankings stored in localStorage — view, re-rank, or delete (includes comparison counts)

## Keyboard Shortcuts (during comparison)

- `←` or `1` — pick left
- `→` or `2` — pick right
- `Ctrl+Z` or `Backspace` — undo

## Deployment

```sh
staticer deploy --domain ranker.baileys.app --expires never --replace
```
