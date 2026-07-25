# compose

Generator for the `baely/infra` service directory a dynamic slop app needs.

Every slop app that ships with a backend gets the same thing added to the infra
repo: `docker/github.com_baely_slop_{app}/` containing a `deploy.yaml` and, if it
has secrets, a `sample.env`. The file follows a fixed pattern each time. This
types it.

Fill in the app name, public domain, container port and the slop commit SHA, and
it live-generates three panes, each with a Copy button:

1. **deploy.yaml** — the compose file. `# Repo:` / `# Ref:` headers, `name: {app}`,
   `image: registry.baileys.dev/{app}:latest`, the traefik router/service labels
   with the domain in the backtick-wrapped `Host()` rule, the optional
   `baileys.public.url/title/description` labels that put the service on
   index.baileys.app, an optional named volume, and the external `web` network.
2. **sample.env** — the committed template, keys only.
3. **Shell steps** — the `docker build --platform linux/amd64 … --push` line, the
   `git add -f` for the gitignored env template, the ssh + `chmod 600` step when
   secrets are on, and `gh pr merge --auto --squash`.

## Notes

- **Secrets never enter this app.** Rows ticked Secret contribute a key to
  `sample.env` with a fixed `changeme` placeholder — the value column is ignored
  for them. The infra repo is public; real values are written by hand on the
  server before the PR merges.
- **Nothing is generated while input is invalid.** App name must be a valid
  compose project name (lowercase alnum and dashes, no leading/trailing dash),
  port must be 1–65535, the domain must be a plausible hostname, the SHA must be
  7–40 hex. The output panes list exactly what is blocking, and each field
  carries its own message.
- **YAML is emitted, not templated loosely.** Environment values are always
  double-quoted with `\` and `"` escaped; labels stay unquoted (matching the
  existing infra files) unless the string would not survive as a plain scalar —
  a `: ` or ` #` or a quote character in a description triggers quoting. Two
  spaces per level, no tabs, no trailing whitespace.
- The form persists to `localStorage`, so a half-filled form survives a reload.
  Reset Form clears it.
- Everything runs client-side. No network requests, no build step.

### Verified against

Feeding voyage's real values in reproduces
`infra/docker/github.com_baely_slop_voyage/deploy.yaml` byte for byte, apart from
the `# Ref:` header (which that file predates) and `name:`, which was manually
set to `traveller` there. Output also round-trips through PyYAML unchanged and
passes `docker compose config`.

## Deployment

```sh
staticer deploy --dir site --domain compose.baileys.dev --expires never
```

Live at https://compose.baileys.dev
