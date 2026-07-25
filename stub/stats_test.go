package main

import (
	"net/http"
	"testing"
	"time"
	"unicode/utf8"
)

func TestClassifyUA(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want string
	}{
		{"chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", bucketBrowser},
		{"safari ios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1", bucketBrowser},
		{"firefox", "Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0", bucketBrowser},
		{"android phone with bot in model", "Mozilla/5.0 (Linux; Android 10; CUBOT_X30) AppleWebKit/537.36 Chrome/100", bucketBrowser},

		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", bucketBot},
		{"bingbot", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", bucketBot},
		{"curl", "curl/8.4.0", bucketBot},
		{"wget", "Wget/1.21.4", bucketBot},
		{"go client", "Go-http-client/2.0", bucketBot},
		{"python", "python-requests/2.31.0", bucketBot},
		{"slack unfurl", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", bucketBot},
		{"facebook unfurl", "facebookexternalhit/1.1", bucketBot},
		{"whatsapp unfurl", "WhatsApp/2.23.20.0", bucketBot},
		{"discord unfurl", "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)", bucketBot},
		{"headless chrome", "Mozilla/5.0 HeadlessChrome/120.0.0.0 Safari/537.36", bucketBot},
		{"generic crawler", "SomeCrawler/1.0 (+https://example.com)", bucketBot},
		{"uptime monitor", "Mozilla/5.0 (compatible; UptimeRobot/2.0; http://uptimerobot.com/)", bucketBot},

		{"empty", "", bucketUnknown},
		{"whitespace", "   ", bucketUnknown},
		{"gibberish", "x", bucketUnknown},
		{"unknown client", "MyInternalTool 1.4", bucketUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyUA(tc.ua, false); got != tc.want {
				t.Fatalf("classifyUA(%q) = %q, want %q", tc.ua, got, tc.want)
			}
		})
	}
}

func TestPrefetchAlwaysCountsAsBot(t *testing.T) {
	realBrowser := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0"
	if got := classifyUA(realBrowser, true); got != bucketBot {
		t.Fatalf("prefetched request from a real browser classified as %q, want %q", got, bucketBot)
	}
}

func TestIsPrefetch(t *testing.T) {
	tests := []struct {
		name   string
		header string
		value  string
		want   bool
	}{
		{"none", "X-Nothing", "", false},
		{"sec-purpose", "Sec-Purpose", "prefetch;prerender", true},
		{"purpose", "Purpose", "prefetch", true},
		{"x-purpose preview", "X-Purpose", "preview", true},
		{"x-moz", "X-Moz", "prefetch", true},
		{"unrelated purpose", "Purpose", "something-else", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set(tc.header, tc.value)
			}
			if got := isPrefetch(h); got != tc.want {
				t.Fatalf("isPrefetch(%s: %s) = %v, want %v", tc.header, tc.value, got, tc.want)
			}
		})
	}
}

func TestBuildChartGeometry(t *testing.T) {
	now := time.Date(2026, 7, 25, 15, 0, 0, 0, time.Local)
	days := map[string]int{
		"2026-07-25": 10, // today
		"2026-07-24": 5,
		"2026-07-01": 1,
		"2026-05-01": 99, // outside the window
	}
	c := buildChart(days, now)

	if len(c.Bars) != chartDays {
		t.Fatalf("bars = %d, want %d", len(c.Bars), chartDays)
	}
	if c.Max != 10 {
		t.Fatalf("max = %d, want 10 (the out-of-window 99 must not scale the axis)", c.Max)
	}
	if c.Total != 16 {
		t.Fatalf("total = %d, want 16", c.Total)
	}
	if c.Empty {
		t.Fatal("chart reported empty despite having clicks")
	}
	if c.Bars[0].Date != "2026-06-26" {
		t.Fatalf("first bar date = %s, want 2026-06-26 (30 days back inclusive)", c.Bars[0].Date)
	}
	last := c.Bars[len(c.Bars)-1]
	if last.Date != "2026-07-25" || last.Count != 10 {
		t.Fatalf("last bar = %+v, want today with 10", last)
	}

	// The tallest bar reaches the plot top; nothing escapes the baseline.
	for _, b := range c.Bars {
		if b.Y+b.H > chartBaseline+0.01 {
			t.Fatalf("bar %s overflows the baseline: y=%v h=%v", b.Date, b.Y, b.H)
		}
		if b.Count > 0 && b.H < 2 {
			t.Fatalf("bar %s with %d clicks has height %v — too small to see", b.Date, b.Count, b.H)
		}
		if b.Count == 0 && b.H != 0 {
			t.Fatalf("empty day %s drew a bar of height %v", b.Date, b.H)
		}
		if b.X < chartLeftPad-0.01 || b.X+b.W > chartW+0.01 {
			t.Fatalf("bar %s sits outside the plot: x=%v w=%v", b.Date, b.X, b.W)
		}
	}
	if last.Y > chartPlotTop+0.01 {
		t.Fatalf("tallest bar top = %v, want it to reach the plot top %v", last.Y, chartPlotTop)
	}
	// Bars must not overlap.
	for i := 1; i < len(c.Bars); i++ {
		if c.Bars[i].X < c.Bars[i-1].X+c.Bars[i-1].W {
			t.Fatalf("bars %d and %d overlap", i-1, i)
		}
	}
}

func TestBuildChartEmpty(t *testing.T) {
	c := buildChart(nil, time.Now())
	if !c.Empty || c.Max != 0 || c.Total != 0 {
		t.Fatalf("empty chart = %+v", struct {
			Empty bool
			Max   int
			Total int
		}{c.Empty, c.Max, c.Total})
	}
	for _, b := range c.Bars {
		if b.H != 0 {
			t.Fatalf("empty chart drew a bar of height %v", b.H)
		}
	}
}

func TestTopReferrers(t *testing.T) {
	m := map[string]int{
		"https://a.example": 10,
		"https://b.example": 5,
		"https://c.example": 5,
		directReferrer:      30,
	}
	rows := topReferrers(m, 3)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].Origin != directReferrer || rows[0].Count != 30 {
		t.Fatalf("first row = %+v, want the biggest bucket", rows[0])
	}
	if rows[0].Pct != 60 {
		t.Fatalf("share = %d%%, want 60", rows[0].Pct)
	}
	// Ties break by name so the table does not reshuffle between renders.
	if rows[2].Origin != "https://b.example" {
		t.Fatalf("tie-break unstable: %+v", rows)
	}
	if len(topReferrers(nil, 5)) != 0 {
		t.Fatal("topReferrers(nil) returned rows")
	}
}

func TestHumanizeTime(t *testing.T) {
	now := time.Now()
	tests := []struct {
		in   time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5 minutes ago"},
		{now.Add(-5 * time.Hour), "5 hours ago"},
		{now.Add(-30 * time.Hour), "yesterday"},
		{now.Add(-72 * time.Hour), "3 days ago"},
		{now.Add(-21 * 24 * time.Hour), "3 weeks ago"},
		{now.Add(2*time.Hour + time.Minute), "in 2 hours"},
	}
	for _, tc := range tests {
		if got := humanizeTime(tc.in); got != tc.want {
			t.Errorf("humanizeTime(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := humanizeAny((*time.Time)(nil)); got != "never" {
		t.Errorf("humanizeAny(nil) = %q, want never", got)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct{ in, want string }{
		{"short", "short"},
		{"exactlyten", "exactlyten"},
		{"more than ten characters", "more than…"},
		{"ünïcödé strïng thät is löng", "ünïcödé s…"},
	}
	for _, tc := range tests {
		got := truncateString(tc.in, 10)
		if got != tc.want {
			t.Errorf("truncateString(%q, 10) = %q, want %q", tc.in, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncateString(%q, 10) produced invalid UTF-8", tc.in)
		}
	}
}
