package server

import (
	"strings"
	"testing"

	"github.com/baileybutler/voyage/internal/store"
)

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"2500", 2500, true},
		{"2,500", 2500, true},
		{"$1,200.50", 1200.50, true},
		{" 90 ", 90, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		got, ok := parseAmount(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseAmount(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNightsBetween(t *testing.T) {
	if n := nightsBetween("2027-04-05", "2027-04-14"); n != 9 {
		t.Errorf("nights = %d want 9", n)
	}
	if n := nightsBetween("2027-04-14", "2027-04-05"); n != 0 {
		t.Errorf("reversed nights = %d want 0", n)
	}
	if n := nightsBetween("", "2027-04-14"); n != 0 {
		t.Errorf("missing start = %d want 0", n)
	}
}

func TestCommaIntAndMoney(t *testing.T) {
	if commaInt(7500) != "7,500" {
		t.Errorf("commaInt(7500) = %q", commaInt(7500))
	}
	if commaInt(999) != "999" {
		t.Errorf("commaInt(999) = %q", commaInt(999))
	}
	if formatMoney("AUD", 1800) != "$1,800" {
		t.Errorf("formatMoney AUD = %q", formatMoney("AUD", 1800))
	}
	if formatMoney("GBP", 2500) != "£2,500" {
		t.Errorf("formatMoney GBP = %q", formatMoney("GBP", 2500))
	}
}

func TestBudgetTotalLabel(t *testing.T) {
	pp := map[string]any{"basis": "pp", "amount": "2500", "currency": "AUD"}
	got := budgetTotalLabel(pp, 3)
	if !strings.Contains(got, "$7,500") || !strings.Contains(got, "3 people") {
		t.Errorf("budgetTotalLabel = %q", got)
	}
	// A total-basis budget shows the implied per-person figure.
	if got := budgetTotalLabel(map[string]any{"basis": "total", "amount": "6000", "currency": "AUD"}, 3); !strings.Contains(got, "$2,000") || !strings.Contains(got, "pp") {
		t.Errorf("total-basis per-person label = %q", got)
	}
	if got := budgetTotalLabel(pp, 1); !strings.Contains(got, "1 person") {
		t.Errorf("singular person label = %q", got)
	}
}

func TestAccomTotalLabel(t *testing.T) {
	night := map[string]any{"basis": "night", "price": "200", "currency": "AUD"}
	got := accomTotalLabel(night, 9)
	if !strings.Contains(got, "$1,800") || !strings.Contains(got, "9 nts") {
		t.Errorf("accomTotalLabel = %q", got)
	}
	if accomTotalLabel(night, 0) != "" {
		t.Error("zero nights should have no total")
	}
	if accomTotalLabel(map[string]any{"basis": "total", "price": "1500"}, 9) != "" {
		t.Error("total-basis stay should have no per-night total")
	}
}

func TestReferenceNights(t *testing.T) {
	// Most-voted date range wins even if shorter.
	dates := []store.AxisOption{
		{Label: "long", Nights: 14, Votes: 1},
		{Label: "popular", Nights: 7, Votes: 5},
	}
	if n, label := referenceNights(dates); n != 7 || label != "popular" {
		t.Errorf("referenceNights = %d,%q want 7,popular", n, label)
	}
	// Tie on votes breaks toward the longest.
	tie := []store.AxisOption{
		{Label: "short", Nights: 3, Votes: 0},
		{Label: "longer", Nights: 10, Votes: 0},
	}
	if n, label := referenceNights(tie); n != 10 || label != "longer" {
		t.Errorf("tie referenceNights = %d,%q want 10,longer", n, label)
	}
	// No usable dates -> 0.
	if n, _ := referenceNights(nil); n != 0 {
		t.Errorf("empty referenceNights = %d want 0", n)
	}
}
