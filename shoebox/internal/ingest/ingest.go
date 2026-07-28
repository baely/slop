// Package ingest turns raw emails (or bare PDFs) into storable receipt
// bundles: headers, first text/html bodies, attachments, and a best-guess
// dollar amount pulled from the text.
package ingest

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime"
	nmail "net/mail"
	"strings"
	"time"

	"github.com/baileybutler/shoebox/internal/store"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

const maxPart = 25 << 20

// FromEmail parses a raw RFC 5322 message. It never fails: unparseable
// messages are kept with whatever headers could be read.
func FromEmail(raw []byte, rcpt string, received time.Time) *store.Bundle {
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

	if mr, err := mail.CreateReader(bytes.NewReader(raw)); err == nil {
		readParts(b, mr)
	} else {
		fallback(b, raw)
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
	if cents, ok := extractAmount(b.Meta.Subject, text); ok {
		b.Meta.AmountCents = cents
		b.Meta.AmountAuto = true
	}
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
