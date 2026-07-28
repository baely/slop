package ingest

import (
	nmail "net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/baileybutler/shoebox/internal/store"
)

// Receipts arrive forwarded, so the top-level headers are the owner's own
// forward, not the merchant's email. Walk everything back to the original:
// sender, recipient, subject, and date. An attached message/rfc822 is
// re-parsed wholesale (rfc822Attachment); inline forwards are mined for the
// client's quoted header block below.

var (
	fwdMarkerRe = regexp.MustCompile(`(?im)(-{3,}\s*Forwarded message\s*-{3,}|^\s*Begin forwarded message:|-+\s*Original Message\s*-+)`)
	fwdSubjRe   = regexp.MustCompile(`(?i)^\s*(?:fwd?|fw)\s*:\s*`)
	hFromRe     = regexp.MustCompile(`(?i)^\s*>?\s*From:\s*(.+)$`)
	hToRe       = regexp.MustCompile(`(?i)^\s*>?\s*To:\s*(.+)$`)
	hDateRe     = regexp.MustCompile(`(?i)^\s*>?\s*(?:Date|Sent):\s*(.+)$`)
	hSubjRe     = regexp.MustCompile(`(?i)^\s*>?\s*Subject:\s*(.+)$`)
	addrRe      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

	// gmail writes U+202F before AM/PM; html mail is full of NBSPs. Fold
	// every unicode space to a plain one before any matching or parsing.
	spaceFold = strings.NewReplacer(" ", " ", " ", " ", " ", " ", " ", " ", " ", " ")
)

func normSpaces(s string) string { return spaceFold.Replace(s) }

// Quoted-header date shapes seen in gmail / apple mail / outlook forwards
// (tried after RFC 5322 parsing, in the app's timezone).
var fwdDateLayouts = []string{
	"Mon, 2 Jan 2006 at 15:04",
	"Mon, 2 Jan 2006 at 3:04 PM",
	"Mon, 2 Jan 2006 at 3:04 pm",
	"Mon, Jan 2, 2006 at 3:04 PM",
	"Mon, Jan 2, 2006 at 3:04 pm",
	"2 Jan 2006, 15:04",
	"2 Jan 2006 15:04",
	"Monday, 2 January 2006 3:04 PM",
	"Monday, January 2, 2006 3:04 PM",
	"2 January 2006 at 15:04:05 GMT-7",
}

func walkBackForward(b *store.Bundle, loc *time.Location) {
	text := string(b.BodyText)
	if text == "" && len(b.BodyHTML) > 0 {
		text = stripHTML(string(b.BodyHTML))
	}
	if text == "" {
		return
	}
	text = normSpaces(text)

	window := forwardWindow(text, b.Meta.Subject)
	if window == nil {
		return
	}

	var fromLine, toLine, dateLine, subjLine string
	for _, line := range window {
		switch {
		case fromLine == "" && hFromRe.MatchString(line):
			fromLine = strings.TrimSpace(hFromRe.FindStringSubmatch(line)[1])
		case toLine == "" && hToRe.MatchString(line):
			toLine = strings.TrimSpace(hToRe.FindStringSubmatch(line)[1])
		case dateLine == "" && hDateRe.MatchString(line):
			dateLine = strings.TrimSpace(hDateRe.FindStringSubmatch(line)[1])
		case subjLine == "" && hSubjRe.MatchString(line):
			subjLine = strings.TrimSpace(hSubjRe.FindStringSubmatch(line)[1])
		}
	}

	addr, name := parseAddrLine(fromLine)
	if addr == "" || strings.EqualFold(addr, b.Meta.From) {
		return
	}

	b.Meta.ForwardedBy = b.Meta.From
	b.Meta.From = addr
	b.Meta.FromName = name
	if to, _ := parseAddrLine(toLine); to != "" {
		b.Meta.To = to
	}
	if subjLine != "" {
		b.Meta.Subject = subjLine
	} else {
		b.Meta.Subject = fwdSubjRe.ReplaceAllString(b.Meta.Subject, "")
	}
	if d, ok := parseForwardDate(dateLine, loc); ok {
		b.Meta.Date = d
	}
}

// forwardWindow returns the lines to mine for quoted headers: those following
// a forward marker, or — when only the subject says Fwd: — the top of the body.
func forwardWindow(text, subject string) []string {
	const span = 20
	if m := fwdMarkerRe.FindStringIndex(text); m != nil {
		lines := strings.Split(text[m[1]:], "\n")
		if len(lines) > span {
			lines = lines[:span]
		}
		return lines
	}
	if fwdSubjRe.MatchString(subject) {
		lines := strings.Split(text, "\n")
		if len(lines) > span {
			lines = lines[:span]
		}
		return lines
	}
	return nil
}

// parseAddrLine handles both clean RFC 5322 ("Name <a@b.c>") and the
// tag-stripped debris html forwards leave behind ("Name  < a@b.c >").
func parseAddrLine(s string) (addr, name string) {
	if s == "" {
		return "", ""
	}
	if a, err := nmail.ParseAddress(s); err == nil {
		return a.Address, a.Name
	}
	addr = addrRe.FindString(s)
	if addr == "" {
		return "", ""
	}
	name = strings.NewReplacer(addr, "", "<", " ", ">", " ", `"`, " ", "'", " ").Replace(s)
	name = strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	return addr, name
}

func parseForwardDate(s string, loc *time.Location) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	sane := func(t time.Time) bool {
		return t.Year() >= 2000 && t.Before(time.Now().Add(48*time.Hour))
	}
	if t, err := nmail.ParseDate(s); err == nil && sane(t) {
		return t, true
	}
	for _, l := range fwdDateLayouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil && sane(t) {
			return t, true
		}
	}
	return time.Time{}, false
}

// ReparseHeaders re-derives the walked-back header fields from the stored
// raw.eml and persists them if they differ (amounts and notes are never
// touched). Returns whether anything changed — callers should then drop and
// regenerate the email.pdf, whose stamped header is stale.
func ReparseHeaders(st *store.Store, r store.Receipt, loc *time.Location) (bool, error) {
	if !r.HasRaw {
		return false, nil
	}
	raw, err := os.ReadFile(st.RawPath(r.ID))
	if err != nil {
		return false, err
	}
	nb := &store.Bundle{Meta: store.Receipt{ReceivedAt: r.ReceivedAt, AmountCents: -1}}
	parseInto(nb, raw)
	if inner := rfc822Attachment(nb); inner != nil {
		outer := nb.Meta.From
		resetContent(nb)
		parseInto(nb, inner)
		nb.Meta.ForwardedBy = outer
	} else {
		walkBackForward(nb, loc)
	}
	if nb.Meta.Date.IsZero() {
		nb.Meta.Date = r.ReceivedAt
	}
	if nb.Meta.Subject == "" {
		nb.Meta.Subject = "(no subject)"
	}
	if nb.Meta.To == "" {
		nb.Meta.To = r.To
	}

	h := store.Headers{
		From: nb.Meta.From, FromName: nb.Meta.FromName, ForwardedBy: nb.Meta.ForwardedBy,
		To: nb.Meta.To, Subject: nb.Meta.Subject, Date: nb.Meta.Date,
	}
	if h.From == r.From && h.FromName == r.FromName && h.ForwardedBy == r.ForwardedBy &&
		h.To == r.To && h.Subject == r.Subject && h.Date.Equal(r.Date) {
		return false, nil
	}
	return true, st.UpdateHeaders(r.ID, h)
}
