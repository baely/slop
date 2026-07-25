package main

import (
	"strings"
	"testing"
)

func TestSlugAlphabetHasNoAmbiguousCharacters(t *testing.T) {
	for _, bad := range []string{"0", "1", "l", "o", "i", "O", "I", "L"} {
		if strings.Contains(slugAlphabet, bad) {
			t.Fatalf("slugAlphabet contains ambiguous character %q", bad)
		}
	}
	if len(slugAlphabet) != 31 {
		t.Fatalf("slugAlphabet = %d characters, want 31", len(slugAlphabet))
	}
	seen := map[rune]bool{}
	for _, r := range slugAlphabet {
		if seen[r] {
			t.Fatalf("slugAlphabet repeats %q", r)
		}
		seen[r] = true
	}
}

func TestRandomIDShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id, err := randomID(defaultSlugLen)
		if err != nil {
			t.Fatalf("randomID: %v", err)
		}
		if len(id) != defaultSlugLen {
			t.Fatalf("randomID len = %d, want %d", len(id), defaultSlugLen)
		}
		for _, r := range id {
			if !strings.ContainsRune(slugAlphabet, r) {
				t.Fatalf("randomID produced %q which is outside the alphabet", r)
			}
		}
		if seen[id] {
			t.Fatalf("randomID repeated %q within 2000 draws", id)
		}
		seen[id] = true
	}
}

func TestRandomIDCoversWholeAlphabet(t *testing.T) {
	// A biased or truncated generator would leave characters unreachable.
	hit := map[rune]bool{}
	for i := 0; i < 3000; i++ {
		id, err := randomID(8)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range id {
			hit[r] = true
		}
	}
	if len(hit) != len(slugAlphabet) {
		t.Fatalf("only %d of %d alphabet symbols ever generated", len(hit), len(slugAlphabet))
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr string // substring
	}{
		{name: "plain", in: "docs", want: "docs"},
		{name: "mixed case kept", in: "MyLink", want: "MyLink"},
		{name: "hyphen and underscore", in: "a-b_c9", want: "a-b_c9"},
		{name: "surrounding space", in: "  docs  ", want: "docs"},
		{name: "leading slash", in: "/docs", want: "docs"},
		{name: "empty", in: "   ", wantErr: "Slug is empty"},
		{name: "space inside", in: "two words", wantErr: "only letters, digits"},
		{name: "slash inside", in: "a/b", wantErr: "only letters, digits"},
		{name: "dot", in: "a.b", wantErr: "only letters, digits"},
		{name: "too long", in: strings.Repeat("a", maxSlugLen+1), wantErr: "too long"},
		{name: "reserved admin", in: "admin", wantErr: "reserved"},
		{name: "reserved api", in: "api", wantErr: "reserved"},
		{name: "reserved static", in: "static", wantErr: "reserved"},
		{name: "reserved healthz", in: "healthz", wantErr: "reserved"},
		{name: "reserved is case-insensitive", in: "AdMiN", wantErr: "reserved"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSlug(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("normalizeSlug(%q) = %q, want error containing %q", tc.in, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeSlug(%q): unexpected error %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Every path the mux owns must be unreachable as a slug, or a link could
// shadow the UI.
func TestEveryAppPathIsReserved(t *testing.T) {
	for _, p := range []string{"healthz", "static", "admin", "api", "login", "logout", "robots.txt", "favicon.ico"} {
		if _, err := normalizeSlug(p); err == nil {
			t.Fatalf("slug %q was accepted but the app owns that path", p)
		}
	}
}
