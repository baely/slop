package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// expiryOption is one entry in the expiry picker.
type expiryOption struct {
	Value string
	Label string
	TTL   time.Duration
}

// expiryOptions is the whole menu, in display order. A zero TTL means never.
var expiryOptions = []expiryOption{
	{"10m", "10 Minutes", 10 * time.Minute},
	{"1h", "1 Hour", time.Hour},
	{"1d", "1 Day", 24 * time.Hour},
	{"1w", "1 Week", 7 * 24 * time.Hour},
	{"30d", "30 Days", 30 * 24 * time.Hour},
	{"never", "Never", 0},
}

const defaultExpiry = "1w"

var errBadExpiry = errors.New("unknown expiry")

// ParseExpiry maps a form/query value to a TTL. Empty means the default.
func ParseExpiry(v string) (time.Duration, error) {
	if v == "" {
		v = defaultExpiry
	}
	for _, o := range expiryOptions {
		if o.Value == v {
			return o.TTL, nil
		}
	}
	return 0, errBadExpiry
}

// ExpiryValues lists the accepted values, for error messages and the README.
func ExpiryValues() string {
	vals := make([]string, 0, len(expiryOptions))
	for _, o := range expiryOptions {
		vals = append(vals, o.Value)
	}
	return strings.Join(vals, ", ")
}

// HumanSize renders a byte count in decimal units.
func HumanSize(n int) string {
	switch {
	case n == 1:
		return "1 byte"
	case n < 1000:
		return fmt.Sprintf("%d bytes", n)
	case n < 1000*1000:
		return fmt.Sprintf("%.1f kB", float64(n)/1000)
	default:
		return fmt.Sprintf("%.2f MB", float64(n)/1000/1000)
	}
}

// HumanAgo renders a past instant as "4 minutes ago", "yesterday", and so on.
func HumanAgo(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < 45*time.Second:
		return "just now"
	case d < 90*time.Second:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%d years ago", int(d.Hours()/24/365))
	}
}

// HumanDuration renders a remaining span as a bare noun phrase. Unlike the
// past tense, a countdown rounds rather than truncates: a paste created with a
// one day expiry should read "a day", not "23 hours".
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < 45*time.Second:
		return fmt.Sprintf("%d seconds", round(d.Seconds()))
	case d < 90*time.Second:
		return "a minute"
	case d < 45*time.Minute:
		return fmt.Sprintf("%d minutes", round(d.Minutes()))
	case d < 90*time.Minute:
		return "an hour"
	case d < 22*time.Hour:
		return fmt.Sprintf("%d hours", round(d.Hours()))
	case d < 36*time.Hour:
		return "a day"
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d days", round(d.Hours()/24))
	default:
		return fmt.Sprintf("%d months", round(d.Hours()/24/30))
	}
}

func round(f float64) int {
	return int(f + 0.5)
}

// ExpiryLine is the sentence shown under a paste.
func ExpiryLine(p Paste, now time.Time) string {
	var parts []string
	if p.Burn {
		parts = append(parts, "Burns after reading.")
	}
	if p.Expires.IsZero() {
		if !p.Burn {
			parts = append(parts, "Expires never.")
		}
	} else {
		parts = append(parts, fmt.Sprintf("Expires in %s.", HumanDuration(p.Expires.Sub(now))))
	}
	return strings.Join(parts, " ")
}

// ExactTime is the precise timestamp for a title attribute.
func ExactTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05 MST")
}

// SplitLines breaks a body into display lines, normalising CRLF and dropping
// the empty tail a trailing newline produces.
func SplitLines(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimSuffix(body, "\n")
	if body == "" {
		return []string{""}
	}
	return strings.Split(body, "\n")
}
