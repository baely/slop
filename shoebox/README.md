# shoebox

The shoebox you throw tax receipts into, except it's an SMTP server. Forward
receipt emails to `tax@baileys.dev` and get a view of every expense grouped by
AU financial year, with the email and its PDF attachment side by side.

Single Go binary: an SMTP listener for ingest, file-based storage, and a
server-rendered UI. No database, no JavaScript (one `confirm()` on Delete).

## What it does

- **SMTP ingest** (`:2525`): accepts any message it is handed, stores the raw
  `.eml`, and extracts subject/from/date, the first text and HTML bodies, and
  all attachments. Inline images keep working: `cid:` references are rewritten
  to local attachment URLs.
- **Forward walk-back**: forwarded receipts arrive From *you*, so ingest digs
  out the original sender: an attached `message/rfc822` is re-parsed as the
  actual receipt, and inline forwards (gmail's `Forwarded message` block,
  Apple Mail, Outlook) are mined for the quoted `From:` / `Date:` /
  `Subject:`. The original date is used, so receipts land in the right FY;
  the forwarding address is kept as `Forwarded By`.
- **email.pdf**: every email body is snapshotted to PDF (headless chromium)
  and attached to the receipt — the attachment set is the complete archival
  evidence, even when the email itself is the receipt. Generated at ingest,
  and backfilled on startup for receipts that predate the feature.
- **Amount guess**: scans subject + body for dollar amounts; amounts on lines
  mentioning total/amount/paid win. Guesses are flagged `auto` in the UI and
  editable on the receipt page (plus a notes field).
- **Expenses view**: receipts grouped by AU financial year (Jul–Jun,
  `Australia/Melbourne`), with a running total per FY and a count of receipts
  still missing an amount.
- **Receipt view**: email body rendered in a sandboxed iframe (CSP: no
  scripts, no remote loads — tracking pixels stay dead) beside the PDF
  attachment in the browser's viewer. Multiple attachments get tabs. Raw
  `.eml` is downloadable.
- **FY export**: each financial year has an Export link producing a zip —
  every receipt's files under `files/`, and an XLSX at the root (date, from,
  subject, amount, notes) with relative hyperlinks to each receipt PDF and
  email.pdf. Extract and hand the folder to your accountant.
- **Manual upload**: drop in an `.eml` or a bare `.pdf` for receipts that
  never came through mail.
- **Auth**: optional single password (`AUTH_PASSWORD`, 90-day cookie) for
  anywhere the UI is exposed. Unset = open — which is how it deploys, since
  the UI sits behind Traefik's internal-only middleware. The SMTP side is
  always open — routing decides what reaches it.

## Storage

One directory per receipt under `$DATA_DIR/receipts/{id}/`:

```
meta.json     # parsed metadata + amount/notes
raw.eml       # original message, byte for byte
body.html     # first HTML part (cid: rewritten)
body.txt      # first text part
att/          # attachment payloads
```

Grep-able, rsync-able, restore-by-copy. Metadata is held in memory; hundreds
of receipts a year is nothing.

## Run locally

```bash
go run ./cmd/shoebox
# http :8080, smtp :2525, data ./data, no auth
python3 - <<'EOF'
import smtplib
from email.message import EmailMessage
m = EmailMessage()
m["From"], m["To"], m["Subject"] = "shop@example.com", "tax@baileys.dev", "Tax invoice 1"
m.set_content("Total: $12.34")
smtplib.SMTP("127.0.0.1", 2525).send_message(m)
EOF
```

Env: `ADDR` (`:8080`), `SMTP_ADDR` (`:2525`), `DATA_DIR` (`./data`),
`AUTH_PASSWORD` (empty = open), `SMTP_DOMAIN`, `INGEST_ADDR` (display only),
`TZ` (`Australia/Melbourne`), `CHROMIUM_PATH` (email.pdf generation; without
a chromium binary the feature disables itself — on a Mac point it at
`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`). The Docker
image ships alpine + chromium for this, so it's ~400 MB rather than
distroless-tiny.

## Deploy

Via [baely/infra](https://github.com/baely/infra)
(`docker/github.com_baely_slop_shoebox/`):

```bash
docker build --platform linux/amd64 -t registry.baileys.dev/shoebox:latest --push .
# then bump # Ref: in the infra deploy.yaml and merge
```

Web UI: https://shoebox.int.xbd.au (Traefik `internal-only@file`, port 8080).
SMTP: no host port — the service joins the external `postfix_mailnet` network
and is reachable there as `shoebox:2525`, from the postfix mail router only.

## Mail routing (not part of this app)

Nothing arrives over SMTP until mail for `tax@baileys.dev` is routed here.
The postfix mail router on host port 25 (`postfix_mailnet` network) does the
splitting; the transport entry it needs is:

```
tax@baileys.dev   smtp:[shoebox]:2525
```

with a catch-all for everything else (e.g. inbucket). Until that rule exists,
receipts go in via the UI upload.
