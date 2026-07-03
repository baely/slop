package server

import (
	"testing"
	"time"

	"github.com/baileybutler/voyage/internal/store"
)

func lockedRange(start, end string) *store.AxisOption {
	return &store.AxisOption{
		Label:  start + " – " + end,
		Status: "selected",
		Meta:   map[string]any{"start": start, "end": end},
	}
}

func TestBuildTravelDataCountdown(t *testing.T) {
	dates := lockedRange("2026-09-14", "2026-09-21")
	cases := []struct {
		now  string
		want string
	}{
		{"2026-07-03", "73 days to go"},
		{"2026-09-13", "1 day to go"},
		{"2026-09-14", "Day 1 of 8"},
		{"2026-09-21", "Day 8 of 8"},
		{"2026-09-22", "Trip complete"},
	}
	for _, c := range cases {
		now, _ := time.Parse("2006-01-02", c.now)
		d := buildTravelData(dates, nil, nil, nil, now)
		if d.Countdown != c.want {
			t.Errorf("countdown at %s: want %q, got %q", c.now, c.want, d.Countdown)
		}
	}
}

func TestBuildTravelDataLaysOutDaysAndSlots(t *testing.T) {
	dates := lockedRange("2026-09-14", "2026-09-16")
	itin := &store.List{Items: []store.ListItem{
		{ID: 1, Label: "Snorkel", Meta: map[string]any{"day": "2026-09-15", "slot": "afternoon"}},
		{ID: 2, Label: "Breakfast", Meta: map[string]any{"day": "2026-09-15", "slot": "morning"}},
		{ID: 3, Label: "Lost", Meta: map[string]any{"day": "2026-10-01"}},
		{ID: 4, Label: "Someday", Meta: map[string]any{}},
	}}
	acts := []store.ListItem{{ID: 10, Label: "Cloud 9"}, {ID: 2, Label: "unrelated id-space"}}
	now, _ := time.Parse("2006-01-02", "2026-07-03")

	d := buildTravelData(dates, itin, nil, acts, now)
	if len(d.Days) != 3 {
		t.Fatalf("want 3 days, got %d", len(d.Days))
	}
	if d.Days[0].Num != 1 || d.Days[0].Date != "2026-09-14" || d.Days[0].Label != "Mon 14 Sep" {
		t.Fatalf("unexpected day 1: %+v", d.Days[0])
	}
	day2 := d.Days[1]
	if len(day2.Entries) != 2 || day2.Entries[0].Label != "Breakfast" || day2.Entries[1].Label != "Snorkel" {
		t.Fatalf("day 2 entries not slot-ordered: %+v", day2.Entries)
	}
	if len(d.Unscheduled) != 2 {
		t.Fatalf("want 2 unscheduled (out-of-range + no day), got %d", len(d.Unscheduled))
	}
	// No itinerary entry carries an activity ref, so the whole wishlist shows.
	if len(d.Wishlist) != 2 {
		t.Fatalf("want full wishlist, got %d", len(d.Wishlist))
	}
}

func TestBuildTravelDataHidesScheduledActivities(t *testing.T) {
	dates := lockedRange("2026-09-14", "2026-09-16")
	itin := &store.List{Items: []store.ListItem{
		{ID: 1, Label: "Cloud 9", Meta: map[string]any{"day": "2026-09-15", "activity": "10"}},
	}}
	acts := []store.ListItem{{ID: 10, Label: "Cloud 9"}, {ID: 11, Label: "Mud baths"}}
	now, _ := time.Parse("2006-01-02", "2026-07-03")

	d := buildTravelData(dates, itin, nil, acts, now)
	if len(d.Wishlist) != 1 || d.Wishlist[0].ID != 11 {
		t.Fatalf("scheduled activity should leave the wishlist: %+v", d.Wishlist)
	}
}

func TestStayCost(t *testing.T) {
	perNight := map[string]any{"price": "350", "basis": "night"}
	if cost, ok := stayCost(perNight, 7); !ok || cost != 2450 {
		t.Fatalf("per-night: want 2450, got %v (ok=%v)", cost, ok)
	}
	if _, ok := stayCost(perNight, 0); ok {
		t.Fatal("per-night with no nights reference should not price")
	}
	total := map[string]any{"price": "1,800", "basis": "total"}
	if cost, ok := stayCost(total, 0); !ok || cost != 1800 {
		t.Fatalf("total: want 1800, got %v (ok=%v)", cost, ok)
	}
}

func TestBuildBookDataTotalsAgainstLockedBudget(t *testing.T) {
	trip := &store.Trip{PartySize: 2}
	list := &store.List{Items: []store.ListItem{
		{Label: "Flights", Meta: map[string]any{"category": "flight", "status": "booked", "cost": "1400", "currency": "AUD"}},
		{Label: "Resort", Meta: map[string]any{"category": "stay", "status": "todo", "cost": "2450"}},
		{Label: "Ferry", Meta: map[string]any{"category": "transfer", "status": "paid", "cost": "100"}},
	}}
	budget := &store.AxisOption{Status: "selected", Meta: map[string]any{"amount": "2500", "currency": "AUD", "basis": "pp"}}

	d := buildBookData(trip, list, budget, nil, 7)
	if d.BookedLabel != "$1,500" {
		t.Fatalf("booked: want $1,500, got %q", d.BookedLabel)
	}
	if d.PlannedLabel != "$3,950" {
		t.Fatalf("planned: want $3,950, got %q", d.PlannedLabel)
	}
	if d.BudgetLabel != "$5,000" { // 2500 pp × 2 people
		t.Fatalf("budget: want $5,000, got %q", d.BudgetLabel)
	}
	if d.BudgetPct != 30 {
		t.Fatalf("pct: want 30, got %d", d.BudgetPct)
	}
	if len(d.Groups) != 3 || d.Groups[0].Key != "flight" || d.Groups[1].Key != "stay" {
		t.Fatalf("groups out of order: %+v", d.Groups)
	}
}

func TestBuildPlanSummaryNilUntilDecisionsExist(t *testing.T) {
	now, _ := time.Parse("2006-01-02", "2026-07-03")
	if p := buildPlanSummary([]store.AxisOption{{Label: "Sep", Status: "option"}}, nil, nil, nil, now); p != nil {
		t.Fatalf("nothing locked or booked: want nil, got %+v", p)
	}

	dates := []store.AxisOption{*lockedRange("2026-09-14", "2026-09-21")}
	p := buildPlanSummary(dates, nil, nil, nil, now)
	if p == nil || p.Countdown == "" || p.DatesLabel == "" {
		t.Fatalf("locked dates should surface the plan, got %+v", p)
	}
	if len(p.Days) != 0 {
		t.Fatalf("empty days should be omitted from the share view, got %d", len(p.Days))
	}
}

func TestReferenceNightsPrefersLockedRange(t *testing.T) {
	dates := []store.AxisOption{
		{Label: "long", Nights: 14, Votes: 5},
		{Label: "chosen", Nights: 7, Votes: 1, Status: "selected"},
	}
	n, label := referenceNights(dates)
	if n != 7 || label != "chosen" {
		t.Fatalf("want locked range to win, got %d (%s)", n, label)
	}
}
