package ingest

import (
	nmail "net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/baileybutler/shoebox/internal/store"
)

// Receipts arrive forwarded, so the top-level From is (almost always) the
// owner's own address. Walk the message back to the original sender: an
// attached message/rfc822 is re-parsed wholesale (rfc822Attachment); inline
// forwards are mined for the client's quoted header block below.

var (
	fwdMarkerRe = regexp.MustCompile(`(?im)(-{3,}\s*Forwarded message\s*-{3,}|^\s*Begin forwarded message:|-+\s*Original Message\s*-+)`)
	fwdSubjRe   = regexp.MustCompile(`(?i)^\s*(?:fwd?|fw)\s*:\s*`)
	hFromRe     = regexp.MustCompile(`(?i)^\s*>?\s*From:\s*(.+)$`)
	hDateRe     = regexp.MustCompile(`(?i)^\s*>?\s*(?:Date|Sent):\s*(.+)$`)
	hSubjRe     = regexp.MustCompile(`(?i)^\s*>?\s*Subject:\s*(.+)$`)
	addrRe      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

// Quoted-header date shapes seen in gmail / apple mail / outlook forwards
// (tried after RFC 5322 parsing, in the app's timezone).
var fwdDateLayouts = []string{
	"Mon, 2 Jan 2006 at 15:04",
	"Mon, 2 Jan 2006 at 3:04 PM",
	"Mon, 2 Jan 2006 at 3:04 pm",
	"Mon, Jan 2, 2006 at 3:04 PM",
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

	window := forwardWindow(text, b.Meta.Subject)
	if window == nil {
		return
	}

	var fromLine, dateLine, subjLine string
	for _, line := range window {
		if fromLine == "" {
			if m := hFromRe.FindStringSubmatch(line); m != nil {
				fromLine = strings.TrimSpace(m[1])
				continue
			}
		}
		if dateLine == "" {
			if m := hDateRe.FindStringSubmatch(line); m != nil {
				dateLine = strings.TrimSpace(m[1])
				continue
			}
		}
		if subjLine == "" {
			if m := hSubjRe.FindStringSubmatch(line); m != nil {
				subjLine = strings.TrimSpace(m[1])
			}
		}
	}

	addr, name := parseFromLine(fromLine)
	if addr == "" || strings.EqualFold(addr, b.Meta.From) {
		return
	}

	b.Meta.ForwardedBy = b.Meta.From
	b.Meta.From = addr
	b.Meta.FromName = name
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

// parseFromLine handles both clean RFC 5322 ("Name <a@b.c>") and the
// tag-stripped debris html forwards leave behind ("Name  < a@b.c >").
func parseFromLine(s string) (addr, name string) {
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
