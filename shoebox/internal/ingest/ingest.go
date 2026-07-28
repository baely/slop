// Package ingest turns raw emails (or bare PDFs) into storable receipt
// bundles: headers, first text/html bodies, attachments, a best-guess dollar
// amount, the original sender walked back out of forwards, and a generated
// email.pdf snapshot of the body.
package ingest

import (
	"bytes"
	"encoding/base64"
	"fmt"
	htmlpkg "html"
	"io"
	"log"
	"mime"
	nmail "net/mail"
	"os"
	"strings"
	"time"

	"github.com/baileybutler/shoebox/internal/pdfgen"
	"github.com/baileybutler/shoebox/internal/store"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

const maxPart = 25 << 20

// FromEmail parses a raw RFC 5322 message. It never fails: unparseable
// messages are kept with whatever headers could be read.
func FromEmail(raw []byte, rcpt string, received time.Time, loc *time.Location) *store.Bundle {
	b := &store.Bundle{
		Raw: raw,
		Meta: store.Receipt{
			To:          rcpt,
			ReceivedAt:  received,
			Source:      "smtp",
			AmountCents: -1,
			HasRaw:      true,
		},
	}
	parseInto(b, raw)

	// Forwards: the top-level From is the owner, not the merchant.
	if inner := rfc822Attachment(b); inner != nil {
		outerFrom := b.Meta.From
		resetContent(b)
		parseInto(b, inner)
		b.Meta.ForwardedBy = outerFrom
	} else {
		walkBackForward(b, loc)
	}

	if b.Meta.Date.IsZero() {
		b.Meta.Date = received
	}
	if b.Meta.Subject == "" {
		b.Meta.Subject = "(no subject)"
	}

	text := string(b.BodyText)
	if text == "" && len(b.BodyHTML) > 0 {
		text = stripHTML(string(b.BodyHTML))
	}
	text = normSpaces(text)
	if cents, ok := extractAmount(b.Meta.Subject, text); ok {
		b.Meta.AmountCents = cents
		b.Meta.AmountAuto = true
	}

	attachEmailPDF(b)
	return b
}

// FromPDF wraps a bare uploaded PDF as a receipt with a single attachment.
func FromPDF(filename string, data []byte, received time.Time) *store.Bundle {
	san := sanitizeName(filename)
	if !strings.HasSuffix(strings.ToLower(san), ".pdf") {
		san += ".pdf"
	}
	file := "0_" + san
	return &store.Bundle{
		Meta: store.Receipt{
			Subject:     strings.TrimSuffix(san, ".pdf"),
			From:        "manual upload",
			Date:        received,
			ReceivedAt:  received,
			Source:      "upload",
			AmountCents: -1,
			Attachments: []store.Attachment{{
				File: file, Name: san, ContentType: "application/pdf", Size: int64(len(data)),
			}},
		},
		Files: []store.File{{Name: file, Data: data}},
	}
}

func parseInto(b *store.Bundle, raw []byte) {
	if mr, err := mail.CreateReader(bytes.NewReader(raw)); err == nil {
		readParts(b, mr)
	} else {
		fallback(b, raw)
	}
}

// rfc822Attachment returns the payload of the first attached email, if any
// (forward-as-attachment) — that inner message is the actual receipt.
func rfc822Attachment(b *store.Bundle) []byte {
	for _, a := range b.Meta.Attachments {
		if a.ContentType == "message/rfc822" {
			for _, f := range b.Files {
				if f.Name == a.File {
					return f.Data
				}
			}
		}
	}
	return nil
}

// resetContent clears everything parseInto fills, keeping envelope facts
// (To, ReceivedAt, Source, Raw).
func resetContent(b *store.Bundle) {
	b.BodyHTML, b.BodyText, b.Files = nil, nil, nil
	b.Meta.From, b.Meta.FromName, b.Meta.Subject = "", "", ""
	b.Meta.Date = time.Time{}
	b.Meta.HasHTML, b.Meta.HasText = false, false
	b.Meta.Attachments = nil
}

func readParts(b *store.Bundle, mr *mail.Reader) {
	h := mr.Header
	if subj, err := h.Subject(); err == nil {
		b.Meta.Subject = strings.TrimSpace(subj)
	}
	if list, err := h.AddressList("From"); err == nil && len(list) > 0 {
		b.Meta.From = list[0].Address
		b.Meta.FromName = list[0].Name
	}
	if d, err := h.Date(); err == nil {
		b.Meta.Date = d
	}
	if b.Meta.To == "" {
		if list, err := h.AddressList("To"); err == nil && len(list) > 0 {
			b.Meta.To = list[0].Address
		}
	}

	idx := 0
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("ingest: part error: %v", err)
			break
		}
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, params, _ := ph.ContentType()
			switch {
			case ct == "text/plain" && len(b.BodyText) == 0:
				b.BodyText, _ = io.ReadAll(io.LimitReader(p.Body, maxPart))
			case ct == "text/html" && len(b.BodyHTML) == 0:
				b.BodyHTML, _ = io.ReadAll(io.LimitReader(p.Body, maxPart))
			case strings.HasPrefix(ct, "text/"):
				// extra text alternative, skip
			default:
				addFile(b, &idx, p.Body, ct, params["name"], contentID(ph.Get("Content-Id")), true)
			}
		case *mail.AttachmentHeader:
			ct, params, _ := ph.ContentType()
			name, _ := ph.Filename()
			if name == "" {
				name = params["name"]
			}
			addFile(b, &idx, p.Body, ct, name, contentID(ph.Get("Content-Id")), false)
		}
	}

	b.Meta.HasHTML = len(b.BodyHTML) > 0
	b.Meta.HasText = len(b.BodyText) > 0

	// Point cid: references in the HTML at our attachment URLs (relative, so
	// they resolve under /r/{id}/).
	for _, a := range b.Meta.Attachments {
		if a.ContentID != "" && len(b.BodyHTML) > 0 {
			b.BodyHTML = bytes.ReplaceAll(b.BodyHTML, []byte("cid:"+a.ContentID), []byte("att/"+a.File))
		}
	}
}

func addFile(b *store.Bundle, idx *int, body io.Reader, ct, name, cid string, inline bool) {
	data, err := io.ReadAll(io.LimitReader(body, maxPart))
	if err != nil || len(data) == 0 {
		return
	}
	if name == "" {
		if cid != "" {
			name = cid
		} else {
			name = "attachment"
		}
		name += extFor(ct)
	}
	san := sanitizeName(name)
	file := fmt.Sprintf("%d_%s", *idx, san)
	*idx++
	b.Meta.Attachments = append(b.Meta.Attachments, store.Attachment{
		File: file, Name: san, ContentType: ct, Size: int64(len(data)), ContentID: cid, Inline: inline,
	})
	b.Files = append(b.Files, store.File{Name: file, Data: data})
}

// fallback pulls what it can with net/mail when full MIME parsing fails.
func fallback(b *store.Bundle, raw []byte) {
	msg, err := nmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		b.Meta.Subject = "(unparseable message)"
		return
	}
	dec := new(mime.WordDecoder)
	if s := msg.Header.Get("Subject"); s != "" {
		if d, err := dec.DecodeHeader(s); err == nil {
			b.Meta.Subject = d
		} else {
			b.Meta.Subject = s
		}
	}
	if a, err := nmail.ParseAddress(msg.Header.Get("From")); err == nil {
		b.Meta.From, b.Meta.FromName = a.Address, a.Name
	}
	if d, err := msg.Header.Date(); err == nil {
		b.Meta.Date = d
	}
	body, _ := io.ReadAll(io.LimitReader(msg.Body, maxPart))
	if len(body) > 0 && !bytes.Contains(body, []byte{0}) {
		b.BodyText = body
		b.Meta.HasText = true
	}
}

// --- generated email.pdf ---

// attachEmailPDF snapshots the email body as a PDF attachment so every
// receipt's attachment set is the complete archival evidence.
func attachEmailPDF(b *store.Bundle) {
	if !pdfgen.Available() {
		return
	}
	src := conversionHTML(b.Meta, b.BodyHTML, b.BodyText, func(file string) []byte {
		for _, f := range b.Files {
			if f.Name == file {
				return f.Data
			}
		}
		return nil
	})
	if src == nil {
		return
	}
	pdf, err := pdfgen.HTMLToPDF(src)
	if err != nil {
		log.Printf("ingest: email pdf failed: %v", err)
		return
	}
	file := fmt.Sprintf("%d_email.pdf", len(b.Meta.Attachments))
	b.Meta.Attachments = append(b.Meta.Attachments, store.Attachment{
		File: file, Name: "email.pdf", ContentType: "application/pdf",
		Size: int64(len(pdf)), Generated: true,
	})
	b.Files = append(b.Files, store.File{Name: file, Data: pdf})
}

// BackfillPDF generates the email.pdf for an already-stored receipt that
// predates the feature. No-op when a generated pdf exists or there's no body.
func BackfillPDF(st *store.Store, r store.Receipt) error {
	if !pdfgen.Available() || (!r.HasHTML && !r.HasText) {
		return nil
	}
	for _, a := range r.Attachments {
		if a.Generated || a.Name == "email.pdf" {
			return nil
		}
	}
	var html, text []byte
	if r.HasHTML {
		html, _ = os.ReadFile(st.BodyPath(r.ID, true))
	}
	if r.HasText {
		text, _ = os.ReadFile(st.BodyPath(r.ID, false))
	}
	src := conversionHTML(r, html, text, func(file string) []byte {
		data, _ := os.ReadFile(st.AttPath(r.ID, file))
		return data
	})
	if src == nil {
		return nil
	}
	pdf, err := pdfgen.HTMLToPDF(src)
	if err != nil {
		return err
	}
	att := store.Attachment{
		File: fmt.Sprintf("%d_email.pdf", len(r.Attachments)), Name: "email.pdf",
		ContentType: "application/pdf", Size: int64(len(pdf)), Generated: true,
	}
	return st.AddAttachment(r.ID, att, pdf)
}

// conversionHTML builds the document handed to chromium: a small archival
// header, then the HTML body with inline cid images embedded as data: URIs
// (the stored body references them by att/ URL, which file:// can't reach).
func conversionHTML(m store.Receipt, html, text []byte, readFile func(file string) []byte) []byte {
	var body []byte
	switch {
	case len(html) > 0:
		body = html
		for _, a := range m.Attachments {
			if !a.Inline || a.ContentID == "" {
				continue
			}
			if data := readFile(a.File); data != nil {
				uri := "data:" + a.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
				body = bytes.ReplaceAll(body, []byte("att/"+a.File), []byte(uri))
			}
		}
	case len(text) > 0:
		body = []byte(`<pre style="font:12px/1.5 monospace;white-space:pre-wrap">` +
			htmlpkg.EscapeString(string(text)) + `</pre>`)
	default:
		return nil
	}
	return append([]byte(headerBlock(m)), body...)
}

func headerBlock(m store.Receipt) string {
	esc := htmlpkg.EscapeString
	from := m.From
	if m.FromName != "" {
		from = m.FromName + " <" + m.From + ">"
	}
	fwd := ""
	if m.ForwardedBy != "" {
		fwd = "<br>Forwarded by: " + esc(m.ForwardedBy)
	}
	return fmt.Sprintf(`<div style="font:11px/1.6 monospace;color:#444;border-bottom:1px solid #ccc;padding:0 0 8px;margin:0 0 12px">From: %s<br>Subject: %s<br>Date: %s%s</div>`,
		esc(from), esc(m.Subject), esc(m.Date.Format("Mon, 2 Jan 2006 15:04 -0700")), fwd)
}

func contentID(v string) string {
	return strings.Trim(strings.TrimSpace(v), "<>")
}

func extFor(ct string) string {
	switch ct {
	case "application/pdf":
		return ".pdf"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "message/rfc822":
		return ".eml"
	}
	if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	out := strings.Trim(sb.String(), "._")
	if len(out) > 80 {
		ext := ""
		if i := strings.LastIndex(out, "."); i > len(out)-8 {
			ext = out[i:]
		}
		out = out[:80-len(ext)] + ext
	}
	if out == "" {
		out = "file"
	}
	return out
}
