package main

import (
	"strings"
	"testing"
)

func TestParseLinks(t *testing.T) {
	in := `
# comment
linkedin=https://linkedin.com/in/baileybutler1
  GitHub = https://github.com/baely
=https://baileybutler.com
docs/api=https://example.com/docs?x=1&y=2
mail=mailto:hi@example.com
`
	links, err := ParseLinks(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Link{
		{"/linkedin", "https://linkedin.com/in/baileybutler1"},
		{"/GitHub", "https://github.com/baely"},
		{"/", "https://baileybutler.com"},
		{"/docs/api", "https://example.com/docs?x=1&y=2"},
		{"/mail", "mailto:hi@example.com"},
	}
	if len(links) != len(want) {
		t.Fatalf("got %d links, want %d: %+v", len(links), len(want), links)
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, links[i], want[i])
		}
	}
}

func TestParseLinksErrors(t *testing.T) {
	cases := map[string]string{
		"no equals":      "linkedin https://x.com",
		"duplicate":      "a=https://x.com\na=https://y.com",
		"relative url":   "a=/somewhere",
		"bare host":      "a=example.com",
		"space in url":   "a=https://x.com/a b",
		"crlf injection": "a=https://x.com/\r\nSet-Cookie: x",
		"non-ascii url":  "a=https://x.com/café",
		"bad key char":   "a?b=https://x.com",
		"leading slash":  "/a=https://x.com",
		"empty url":      "a=",
		"scheme only":    "a=https:",
	}
	for name, in := range cases {
		if _, err := ParseLinks(strings.NewReader(in)); err == nil {
			t.Errorf("%s: expected error for %q", name, in)
		}
	}
}

func TestKeyPath(t *testing.T) {
	cases := map[string]string{"": "/", "linkedin": "/linkedin", "LinkedIn": "/LinkedIn", "a/B/": "/a/B/", "x.y~z_w-v": "/x.y~z_w-v"}
	for in, want := range cases {
		got, err := keyPath(in)
		if err != nil || got != want {
			t.Errorf("keyPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
