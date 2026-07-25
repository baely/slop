package main

import (
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// botSubstrings are unambiguous: no browser ships a User-Agent containing
// these. Matched anywhere in the string.
var botSubstrings = []string{
	"slurp", "curl/", "wget/", "libwww-perl", "python-requests", "python-urllib",
	"go-http-client", "java/", "okhttp", "axios", "node-fetch", "httpie",
	"powershell", "postmanruntime", "insomnia", "restsharp", "guzzle",
	"apache-httpclient", "headlesschrome", "phantomjs", "puppeteer",
	"playwright", "selenium", "chrome-lighthouse", "pingdom", "uptimerobot",
	"statuscake", "site24x7", "healthcheck", "prerender", "http_request",
	"facebookexternalhit", "whatsapp", "telegrambot", "slackbot",
	"slack-imgproxy", "discordbot", "twitterbot", "linkedinbot",
	"skypeuripreview", "embedly", "quora link preview", "redditbot",
	"applebot", "bingpreview", "google-inspectiontool", "feedfetcher",
	"w3c_validator", "checkmk", "zabbix", "nagios", "ahrefs", "semrush",
	"petalbot", "yandex", "duckduckgo-favicons",
}

// botWords need a trailing delimiter so "Googlebot/2.1" matches while a phone
// model like "CUBOT_X30" does not. This is a heuristic, not a bot database —
// see the note in the README.
var botWords = []string{
	"bot", "crawler", "spider", "scraper", "archiver", "fetcher",
	"monitoring", "validator", "preview",
}

// wordDelims end a botWord match. '_' and '-' are excluded on purpose: they
// show up inside hardware model names far more often than in crawler names.
const wordDelims = " /;)(,'\"+|]"

func matchesBotWord(lower string) bool {
	for _, w := range botWords {
		for i := 0; ; {
			j := strings.Index(lower[i:], w)
			if j < 0 {
				break
			}
			end := i + j + len(w)
			if end == len(lower) || strings.IndexByte(wordDelims, lower[end]) >= 0 {
				return true
			}
			i = i + j + 1
			if i >= len(lower) {
				break
			}
		}
	}
	return false
}

// classifyUA buckets a request as browser, bot or unknown. Prefetch and
// preview hints win outright: a browser speculatively warming a link is not a
// person choosing to follow it.
func classifyUA(ua string, prefetch bool) string {
	if prefetch {
		return bucketBot
	}
	trimmed := strings.TrimSpace(ua)
	if trimmed == "" {
		return bucketUnknown
	}
	lower := strings.ToLower(trimmed)
	for _, tok := range botSubstrings {
		if strings.Contains(lower, tok) {
			return bucketBot
		}
	}
	if matchesBotWord(lower) {
		return bucketBot
	}
	if strings.HasPrefix(lower, "mozilla/") || strings.HasPrefix(lower, "opera/") {
		return bucketBrowser
	}
	return bucketUnknown
}

// isPrefetch spots the standard "I am warming this link, nobody clicked it"
// signals. Chrome/Safari send Sec-Purpose or Purpose; Firefox sends X-moz.
func isPrefetch(h http.Header) bool {
	hints := []string{
		h.Get("Sec-Purpose"),
		h.Get("Purpose"),
		h.Get("X-Purpose"),
		h.Get("X-Moz"),
		h.Get("Sec-Fetch-Purpose"),
	}
	for _, v := range hints {
		v = strings.ToLower(v)
		if strings.Contains(v, "prefetch") || strings.Contains(v, "preview") || strings.Contains(v, "prerender") {
			return true
		}
	}
	return false
}

// ---- chart ----

// Bar is one column of the 30-day chart, in SVG user units.
type Bar struct {
	X, Y, W, H float64
	Date       string
	Label      string
	Count      int
}

// Chart is a hand-built SVG bar chart: single teal series, one y-axis, no
// gridlines beyond the baseline. All geometry is computed here so the template
// holds no magic numbers. Values are rendered in mono by the CSS.
type Chart struct {
	Bars      []Bar
	Max       int
	Total     int
	Width     float64
	Height    float64
	LeftPad   float64
	Baseline  float64
	TopTickY  float64
	ZeroTickY float64
	XTickY    float64
	FromLabel string
	ToLabel   string
	Empty     bool
}

const (
	chartW        = 640.0
	chartPlotTop  = 10.0
	chartBaseline = 150.0
	chartH        = 172.0
	chartLeftPad  = 34.0
)

// buildChart lays out the last `chartDays` days ending at now (local time).
func buildChart(days map[string]int, now time.Time) Chart {
	c := Chart{
		Width:     chartW,
		Height:    chartH,
		LeftPad:   chartLeftPad,
		Baseline:  chartBaseline,
		TopTickY:  chartPlotTop + 4,
		ZeroTickY: chartBaseline + 4,
		XTickY:    chartH - 4,
	}
	plotW := chartW - chartLeftPad
	gap := 3.0
	barW := (plotW - gap*float64(chartDays-1)) / float64(chartDays)
	plotH := chartBaseline - chartPlotTop

	start := now.AddDate(0, 0, -(chartDays - 1))
	for i := 0; i < chartDays; i++ {
		d := start.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		n := days[key]
		if n > c.Max {
			c.Max = n
		}
		c.Total += n
		c.Bars = append(c.Bars, Bar{
			X:     chartLeftPad + float64(i)*(barW+gap),
			W:     barW,
			Date:  key,
			Label: d.Format("2 Jan"),
			Count: n,
		})
	}
	c.FromLabel = start.Format("2 Jan")
	c.ToLabel = now.Format("2 Jan")
	c.Empty = c.Max == 0

	scale := c.Max
	if scale == 0 {
		scale = 1
	}
	for i := range c.Bars {
		h := plotH * float64(c.Bars[i].Count) / float64(scale)
		if c.Bars[i].Count > 0 && h < 2 {
			h = 2 // a single click must still be visible
		}
		c.Bars[i].H = round2(h)
		c.Bars[i].Y = round2(chartBaseline - h)
		c.Bars[i].X = round2(c.Bars[i].X)
		c.Bars[i].W = round2(c.Bars[i].W)
	}
	return c
}

// round2 keeps the generated SVG readable instead of full of 53.166666666666664.
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// RefRow is one row of the referrer breakdown.
type RefRow struct {
	Origin string
	Count  int
	Pct    int
}

// topReferrers returns the busiest origins, largest first, capped to n.
func topReferrers(m map[string]int, n int) []RefRow {
	total := 0
	rows := make([]RefRow, 0, len(m))
	for k, v := range m {
		rows = append(rows, RefRow{Origin: k, Count: v})
		total += v
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Origin < rows[j].Origin
	})
	if len(rows) > n {
		rows = rows[:n]
	}
	for i := range rows {
		if total > 0 {
			rows[i].Pct = int(float64(rows[i].Count)*100/float64(total) + 0.5)
		}
	}
	return rows
}
