package main

import (
	"strings"
	"testing"
)

func TestAlphabet(t *testing.T) {
	cases := []struct {
		r    rules
		want string
	}{
		{rules{Length: 6, Lower: true}, "abcdefghijklmnopqrstuvwxyz"},
		{rules{Length: 6, Lower: true, Unambiguous: true}, "abcdefghijkmnopqrstuvwxyz"},
		{rules{Length: 6, Digits: true, Unambiguous: true}, "23456789"},
		{rules{Length: 6, Upper: true, Unambiguous: true}, "ABCDEFGHJKLMNPQRSTUVWXYZ"},
		{rules{Length: 6, Lower: true, Upper: true, Digits: true}, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"},
	}
	for _, c := range cases {
		got, err := c.r.alphabet()
		if err != nil || got != c.want {
			t.Errorf("%+v: %q %v, want %q", c.r, got, err, c.want)
		}
	}
	for _, bad := range []rules{{Length: 6}, {Length: 0, Lower: true}, {Length: 33, Lower: true}} {
		if _, err := bad.alphabet(); err == nil {
			t.Errorf("%+v: expected error", bad)
		}
	}
}

func TestGenerate(t *testing.T) {
	r := rules{Length: 8, Lower: true, Digits: true, Unambiguous: true}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		k, err := generate(r, func(k string) bool { return seen[k] })
		if err != nil {
			t.Fatal(err)
		}
		if len(k) != 8 || strings.ContainsAny(k, ambiguous+"ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			t.Fatalf("bad key %q", k)
		}
		if seen[k] {
			t.Fatalf("duplicate %q", k)
		}
		seen[k] = true
	}
	// With everything taken, it gives up instead of spinning.
	if _, err := generate(rules{Length: 1, Digits: true}, func(string) bool { return true }); err == nil {
		t.Fatal("expected exhaustion error")
	}
}
