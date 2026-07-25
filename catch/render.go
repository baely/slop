package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// bodyKind tells the template which shape to draw.
type bodyKind string

const (
	bodyEmpty bodyKind = "empty"
	bodyJSON  bodyKind = "json"
	bodyForm  bodyKind = "form"
	bodyText  bodyKind = "text"
	bodyHex   bodyKind = "hex"
)

// Pair is one decoded form field.
type Pair struct {
	Key   string
	Value string
}

// BodyView is the presentation of a captured body. Text is always plain text;
// html/template escapes it on the way out.
type BodyView struct {
	Kind  bodyKind
	Text  string
	Pairs []Pair
	Notes []string
}

const truncatedNote = "Body truncated at 64 KB."

// viewBody decides how to show a captured body: pretty JSON where it parses,
// a decoded table for form posts, a hex dump for anything that is not valid
// UTF-8, and raw text otherwise.
func viewBody(req *Request) BodyView {
	v := BodyView{}
	if req.Truncated {
		v.Notes = append(v.Notes, truncatedNote)
	}
	if len(req.Body) == 0 {
		v.Kind = bodyEmpty
		return v
	}
	if !utf8.Valid(req.Body) {
		v.Kind = bodyHex
		v.Text = hexDump(req.Body)
		v.Notes = append(v.Notes, "Body is not valid UTF-8. Shown as a hex dump.")
		return v
	}

	text := string(req.Body)
	base := mediaType(req.ContentType)

	switch {
	case looksJSON(base, req.Body):
		if pretty, ok := prettyJSON(req.Body); ok {
			v.Kind = bodyJSON
			v.Text = pretty
			return v
		}
		v.Kind = bodyText
		v.Text = text
		v.Notes = append(v.Notes, "Body is not valid JSON.")
		return v
	case base == "application/x-www-form-urlencoded":
		if pairs, ok := parseForm(text); ok {
			v.Kind = bodyForm
			v.Pairs = pairs
			v.Text = text
			return v
		}
	}
	v.Kind = bodyText
	v.Text = text
	return v
}

func mediaType(ct string) string {
	if ct == "" {
		return ""
	}
	base, _, err := mime.ParseMediaType(ct)
	if err != nil {
		base, _, _ = strings.Cut(ct, ";")
		return strings.ToLower(strings.TrimSpace(base))
	}
	return strings.ToLower(base)
}

// looksJSON is true for JSON-ish content types and for bodies that open with a
// JSON object or array whatever the sender claimed.
func looksJSON(base string, body []byte) bool {
	if base == "application/json" || base == "text/json" ||
		strings.HasSuffix(base, "+json") {
		return true
	}
	t := bytes.TrimLeft(body, " \t\r\n")
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}

func prettyJSON(body []byte) (string, bool) {
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		return "", false
	}
	if !json.Valid(body) {
		return "", false
	}
	return out.String(), true
}

func parseForm(text string) ([]Pair, bool) {
	vals, err := url.ParseQuery(text)
	if err != nil || len(vals) == 0 {
		return nil, false
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]Pair, 0, len(vals))
	for _, k := range keys {
		for _, v := range vals[k] {
			pairs = append(pairs, Pair{Key: k, Value: v})
		}
	}
	return pairs, true
}

func hexDump(b []byte) string {
	const max = 8 << 10 // 8 KB of bytes is plenty on screen
	clipped := false
	if len(b) > max {
		b = b[:max]
		clipped = true
	}
	s := hex.Dump(b)
	if clipped {
		s += fmt.Sprintf("... hex dump clipped at %d bytes.\n", max)
	}
	return s
}

// humanizeAt renders a coarse relative time. Precision lives in the title
// attribute alongside it.
func humanizeAt(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < 10*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/24/7))
	default:
		return fmt.Sprintf("%d years ago", int(d.Hours()/24/365))
	}
}

// untilAt renders a coarse forward-looking duration ("in 12 days").
func untilAt(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "any moment"
	case d < time.Hour:
		return fmt.Sprintf("in %d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("in %d days", int(d.Hours()/24))
	}
}

func formatSize(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d B", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f kB", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/1000/1000)
	}
}

func exactTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02 15:04:05 MST")
}
