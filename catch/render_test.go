package main

import (
	"strings"
	"testing"
	"time"
)

func TestViewBody(t *testing.T) {
	cases := []struct {
		name      string
		req       *Request
		wantKind  bodyKind
		wantText  string   // exact, when set
		wantHas   []string // substrings the text must contain
		wantNotes []string
	}{
		{
			name:     "empty body",
			req:      &Request{},
			wantKind: bodyEmpty,
		},
		{
			name:     "json is re-indented",
			req:      &Request{Body: []byte(`{"bssid":"aa:bb","rssi":-64}`), ContentType: "application/json"},
			wantKind: bodyJSON,
			wantText: "{\n  \"bssid\": \"aa:bb\",\n  \"rssi\": -64\n}",
		},
		{
			name:     "json detected without a content type",
			req:      &Request{Body: []byte(`[1,2]`)},
			wantKind: bodyJSON,
			wantText: "[\n  1,\n  2\n]",
		},
		{
			name:      "broken json says so and shows raw",
			req:       &Request{Body: []byte(`{"a":`), ContentType: "application/json"},
			wantKind:  bodyText,
			wantText:  `{"a":`,
			wantNotes: []string{"Body is not valid JSON."},
		},
		{
			name:      "json content type with prose",
			req:       &Request{Body: []byte(`not json at all`), ContentType: "application/vnd.api+json"},
			wantKind:  bodyText,
			wantNotes: []string{"Body is not valid JSON."},
		},
		{
			name:     "form body is decoded",
			req:      &Request{Body: []byte("device=phone&ssid=home+wifi"), ContentType: "application/x-www-form-urlencoded"},
			wantKind: bodyForm,
		},
		{
			name:     "plain text stays raw",
			req:      &Request{Body: []byte("hello"), ContentType: "text/plain"},
			wantKind: bodyText,
			wantText: "hello",
		},
		{
			name:      "binary becomes a hex dump",
			req:       &Request{Body: []byte{0x00, 0xff, 0xfe, 0x41, 0x42}, ContentType: "application/octet-stream"},
			wantKind:  bodyHex,
			wantHas:   []string{"00000000", "|", "AB"},
			wantNotes: []string{"Body is not valid UTF-8. Shown as a hex dump."},
		},
		{
			name:      "truncated body is flagged",
			req:       &Request{Body: []byte("abc"), BodySize: 100000, Truncated: true, ContentType: "text/plain"},
			wantKind:  bodyText,
			wantNotes: []string{truncatedNote},
		},
		{
			name:     "script tag body is left intact for the escaper",
			req:      &Request{Body: []byte("<script>alert(1)</script>"), ContentType: "text/html"},
			wantKind: bodyText,
			wantText: "<script>alert(1)</script>",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := viewBody(c.req)
			if got.Kind != c.wantKind {
				t.Fatalf("Kind = %q, want %q (text %q)", got.Kind, c.wantKind, got.Text)
			}
			if c.wantText != "" && got.Text != c.wantText {
				t.Fatalf("Text = %q, want %q", got.Text, c.wantText)
			}
			for _, sub := range c.wantHas {
				if !strings.Contains(got.Text, sub) {
					t.Fatalf("Text %q missing %q", got.Text, sub)
				}
			}
			for _, note := range c.wantNotes {
				found := false
				for _, n := range got.Notes {
					if n == note {
						found = true
					}
				}
				if !found {
					t.Fatalf("Notes = %v, want one of them to be %q", got.Notes, note)
				}
			}
			if c.wantNotes == nil && len(got.Notes) != 0 {
				t.Fatalf("unexpected notes %v", got.Notes)
			}
		})
	}
}

func TestViewBodyFormPairs(t *testing.T) {
	v := viewBody(&Request{
		Body:        []byte("ssid=home+wifi&bssid=aa%3Abb&bssid=cc%3Add"),
		ContentType: "application/x-www-form-urlencoded",
	})
	if v.Kind != bodyForm {
		t.Fatalf("Kind = %q", v.Kind)
	}
	want := []Pair{{"bssid", "aa:bb"}, {"bssid", "cc:dd"}, {"ssid", "home wifi"}}
	if len(v.Pairs) != len(want) {
		t.Fatalf("Pairs = %+v, want %+v", v.Pairs, want)
	}
	for i := range want {
		if v.Pairs[i] != want[i] {
			t.Fatalf("Pairs[%d] = %+v, want %+v", i, v.Pairs[i], want[i])
		}
	}
}

func TestMediaType(t *testing.T) {
	cases := map[string]string{
		"":                                  "",
		"application/json":                  "application/json",
		"application/json; charset=utf-8":   "application/json",
		"APPLICATION/JSON":                  "application/json",
		"multipart/form-data; boundary=xyz": "multipart/form-data",
		"nonsense;;;":                       "nonsense",
	}
	for in, want := range cases {
		if got := mediaType(in); got != want {
			t.Errorf("mediaType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanizeAt(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{0, "just now"},
		{3 * time.Second, "just now"},
		{40 * time.Second, "40 seconds ago"},
		{90 * time.Second, "a minute ago"},
		{10 * time.Minute, "10 minutes ago"},
		{90 * time.Minute, "an hour ago"},
		{5 * time.Hour, "5 hours ago"},
		{30 * time.Hour, "yesterday"},
		{3 * 24 * time.Hour, "3 days ago"},
		{40 * 24 * time.Hour, "5 weeks ago"},
		{800 * 24 * time.Hour, "2 years ago"},
	}
	for _, c := range cases {
		if got := humanizeAt(now.Add(-c.ago), now); got != c.want {
			t.Errorf("humanizeAt(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := humanizeAt(time.Time{}, now); got != "never" {
		t.Errorf("humanizeAt(zero) = %q, want %q", got, "never")
	}
	if got := humanizeAt(now.Add(time.Hour), now); got != "just now" {
		t.Errorf("future time = %q, want %q", got, "just now")
	}
}

func TestUntilAt(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-time.Hour, "any moment"},
		{30 * time.Minute, "in 30 minutes"},
		{5 * time.Hour, "in 5 hours"},
		{30 * 24 * time.Hour, "in 30 days"},
	}
	for _, c := range cases {
		if got := untilAt(now.Add(c.in), now); got != c.want {
			t.Errorf("untilAt(+%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{
		0:       "0 B",
		1:       "1 B",
		999:     "999 B",
		1000:    "1.0 kB",
		65536:   "65.5 kB",
		1500000: "1.5 MB",
	}
	for in, want := range cases {
		if got := formatSize(in); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestHexDumpClips(t *testing.T) {
	big := make([]byte, 20<<10)
	for i := range big {
		big[i] = 0xfe
	}
	out := hexDump(big)
	if !strings.Contains(out, "hex dump clipped at 8192 bytes.") {
		t.Fatalf("clip note missing from hex dump tail: %q", out[len(out)-80:])
	}
}
