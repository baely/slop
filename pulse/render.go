package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// humanize renders a timestamp the way the house style asks: words, not dates.
func humanize(t, now time.Time) string {
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

// humanDuration renders an incident length as a compact mono value.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %02dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}

// fmtPct never rounds up to a number the data does not support: 99.96% reads
// as 99.9%, not 100%.
func fmtPct(u Uptime) string {
	if !u.Known {
		return "—"
	}
	p := u.Pct()
	if p >= 100 {
		return "100%"
	}
	return strconv.FormatFloat(math.Floor(p*10)/10, 'f', 1, 64) + "%"
}

func fmtExact(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02 15:04:05 MST")
}

// ---------- sparkline ----------

// SparkRect is a pre-computed SVG rectangle (strings so the template does no math).
type SparkRect struct{ X, Y, W, H string }

// Spark is a hand-built latency sparkline: one teal series, thin marks, one
// y-axis expressed as a single max label, failures called out in words.
type Spark struct {
	Has       bool
	HasOK     bool // at least one successful sample, so the peak means something
	Count     int
	CountWord string
	Fails     int
	Paths     []string
	Marks     []SparkRect
	MaxMs     int
	VBW       string
	VBH       string
	Window    string
}

const (
	sparkW    = 240.0
	sparkH    = 34.0
	sparkPadY = 3.0
)

// BuildSpark renders up to n most recent checks as a sparkline.
func BuildSpark(checks []Check, n int) Spark {
	s := Spark{VBW: ftoa(sparkW), VBH: ftoa(sparkH)}
	if len(checks) > n {
		checks = checks[len(checks)-n:]
	}
	s.Count = len(checks)
	s.CountWord = "checks"
	if s.Count == 1 {
		s.CountWord = "check"
	}
	if s.Count == 0 {
		return s
	}
	s.Has = true

	maxMs := 1
	for _, c := range checks {
		if c.OK {
			s.HasOK = true
			if c.LatencyMs > maxMs {
				maxMs = c.LatencyMs
			}
			continue
		}
		s.Fails++
	}
	s.MaxMs = maxMs

	step := sparkW
	if s.Count > 1 {
		step = sparkW / float64(s.Count-1)
	}
	usableH := sparkH - 2*sparkPadY - 3 // 3px reserved for the failure rail
	y := func(ms int) float64 {
		v := float64(ms) / float64(maxMs)
		if v > 1 {
			v = 1
		}
		return sparkPadY + (1-v)*usableH
	}

	var seg []string
	flush := func() {
		switch {
		case len(seg) >= 2:
			s.Paths = append(s.Paths, strings.Join(seg, " "))
		case len(seg) == 1:
			// A lone sample between failures still deserves a mark.
			s.Paths = append(s.Paths, seg[0]+" l0.75,0")
		}
		seg = nil
	}
	for i, c := range checks {
		x := float64(i) * step
		if !c.OK {
			flush()
			s.Marks = append(s.Marks, SparkRect{
				X: ftoa(math.Max(0, x-0.75)),
				Y: ftoa(sparkH - 3),
				W: "1.5",
				H: "3",
			})
			continue
		}
		cmd := "L"
		if len(seg) == 0 {
			cmd = "M"
		}
		seg = append(seg, cmd+ftoa(x)+","+ftoa(y(c.LatencyMs)))
	}
	flush()

	if s.Fails == 1 {
		s.Window = "1 failed"
	} else if s.Fails > 1 {
		s.Window = strconv.Itoa(s.Fails) + " failed"
	}
	return s
}

func ftoa(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}
