package store

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// budgetAndDates returns the seeded budget and dates axes for a trip.
func budgetAndDates(t *testing.T, s *Store, tripID int64) (Axis, Axis) {
	t.Helper()
	axes, err := s.AxesForTrip(tripID)
	if err != nil {
		t.Fatalf("axes: %v", err)
	}
	var b, d Axis
	for _, a := range axes {
		switch a.Kind {
		case "budget":
			b = a
		case "date_range":
			d = a
		}
	}
	if b.ID == 0 || d.ID == 0 {
		t.Fatal("expected seeded budget and dates axes")
	}
	return b, d
}

func TestCreateTripSeedsAxesAndList(t *testing.T) {
	s := newTestStore(t)
	id, err := s.CreateTrip("Italy", []string{"Rome", "Florence"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	b, d := budgetAndDates(t, s, id)
	if b.Name != "Budget" || d.Name != "Dates" {
		t.Fatalf("unexpected axis names: %q %q", b.Name, d.Name)
	}
	if _, err := s.ActivitiesList(id); err != nil {
		t.Fatalf("activities list: %v", err)
	}
	locs, _ := s.LocationsForTrip(id)
	if len(locs) != 2 {
		t.Fatalf("want 2 locations, got %d", len(locs))
	}
}

func TestEnsureComboIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	tripID, _ := s.CreateTrip("Trip", nil)
	budget, dates := budgetAndDates(t, s, tripID)

	bOpt, _ := s.AddAxisOption(budget.ID, "£2500 pp", nil, nil)
	dOpt, _ := s.AddAxisOption(dates.ID, "12–19 Jul", nil, nil)
	sel := map[int64]int64{budget.ID: bOpt, dates.ID: dOpt}

	first, err := s.EnsureCombo(tripID, sel)
	if err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	second, err := s.EnsureCombo(tripID, sel)
	if err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if first != second {
		t.Fatalf("ensure not idempotent: %d != %d", first, second)
	}

	// A different date option must produce a distinct combo.
	dOpt2, _ := s.AddAxisOption(dates.ID, "5–12 Sep", nil, nil)
	other, err := s.EnsureCombo(tripID, map[int64]int64{budget.ID: bOpt, dates.ID: dOpt2})
	if err != nil {
		t.Fatalf("ensure other: %v", err)
	}
	if other == first {
		t.Fatal("distinct selection should yield a distinct combo")
	}

	combos, _ := s.CombosForTrip(tripID)
	if len(combos) != 2 {
		t.Fatalf("want 2 combos, got %d", len(combos))
	}
}

func TestSelectComboItemDemotesSiblings(t *testing.T) {
	s := newTestStore(t)
	tripID, _ := s.CreateTrip("Trip", nil)
	budget, dates := budgetAndDates(t, s, tripID)
	bOpt, _ := s.AddAxisOption(budget.ID, "mid", nil, nil)
	dOpt, _ := s.AddAxisOption(dates.ID, "summer", nil, nil)
	combo, _ := s.EnsureCombo(tripID, map[int64]int64{budget.ID: bOpt, dates.ID: dOpt})

	h1, _ := s.AddComboItem(combo, "hotel", "One", "", "", nil, nil)
	h2, _ := s.AddComboItem(combo, "hotel", "Two", "", "", nil, nil)
	if err := s.SetComboItemStatus(h1, "selected"); err != nil {
		t.Fatalf("select h1: %v", err)
	}
	if err := s.SetComboItemStatus(h2, "selected"); err != nil {
		t.Fatalf("select h2: %v", err)
	}

	got, _ := s.ComboByID(combo)
	selected := 0
	for _, it := range got.Items {
		if it.Status == "selected" {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("want exactly 1 selected hotel, got %d", selected)
	}
}

func TestToggleVoteAndCounts(t *testing.T) {
	s := newTestStore(t)
	tripID, _ := s.CreateTrip("Trip", nil)
	voter, err := s.CreateVoter(tripID, "Alex")
	if err != nil {
		t.Fatalf("voter: %v", err)
	}

	// First toggle adds, second removes.
	if err := s.ToggleVote(tripID, voter.ID, "combo", 7); err != nil {
		t.Fatalf("vote on: %v", err)
	}
	counts, _ := s.VoteCounts(tripID)
	if counts["combo:7"] != 1 {
		t.Fatalf("want 1 vote, got %d", counts["combo:7"])
	}
	if err := s.ToggleVote(tripID, voter.ID, "combo", 7); err != nil {
		t.Fatalf("vote off: %v", err)
	}
	counts, _ = s.VoteCounts(tripID)
	if counts["combo:7"] != 0 {
		t.Fatalf("want 0 votes after toggle off, got %d", counts["combo:7"])
	}

	// A voter only counts once per target even if asked to vote repeatedly.
	_ = s.ToggleVote(tripID, voter.ID, "combo", 7) // on
	byVoter, _ := s.VotesByVoter(voter.ID)
	if !byVoter["combo:7"] {
		t.Fatal("expected voter's vote to be recorded")
	}
}

func TestVoterByTokenScopedToTrip(t *testing.T) {
	s := newTestStore(t)
	tripA, _ := s.CreateTrip("A", nil)
	tripB, _ := s.CreateTrip("B", nil)
	v, _ := s.CreateVoter(tripA, "Sam")

	if _, err := s.VoterByToken(tripA, v.Token); err != nil {
		t.Fatalf("expected voter found for own trip: %v", err)
	}
	if _, err := s.VoterByToken(tripB, v.Token); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound across trips, got %v", err)
	}
}

func TestSelectAxisOptionDemotesSiblings(t *testing.T) {
	s := newTestStore(t)
	tripID, _ := s.CreateTrip("Trip", nil)
	_, dates := budgetAndDates(t, s, tripID)

	d1, _ := s.AddAxisOption(dates.ID, "12–19 Jul", nil, nil)
	d2, _ := s.AddAxisOption(dates.ID, "5–12 Sep", nil, nil)
	if err := s.SetAxisOptionStatus(d1, "selected"); err != nil {
		t.Fatalf("select d1: %v", err)
	}
	if err := s.SetAxisOptionStatus(d2, "selected"); err != nil {
		t.Fatalf("select d2: %v", err)
	}

	_, d := budgetAndDates(t, s, tripID)
	var selected int64
	n := 0
	for _, o := range d.Options {
		if o.Status == "selected" {
			selected = o.ID
			n++
		}
	}
	if n != 1 || selected != d2 {
		t.Fatalf("want exactly d2 selected, got %d selected (id %d)", n, selected)
	}
}

func TestEnsureListIsIdempotentPerKind(t *testing.T) {
	s := newTestStore(t)
	tripID, _ := s.CreateTrip("Trip", nil)

	b1, err := s.EnsureList(tripID, "booking", "Bookings")
	if err != nil {
		t.Fatalf("ensure 1: %v", err)
	}
	b2, err := s.EnsureList(tripID, "booking", "Bookings")
	if err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if b1.ID != b2.ID {
		t.Fatalf("ensure not idempotent: %d != %d", b1.ID, b2.ID)
	}
	p, err := s.EnsureList(tripID, "packing", "Packing")
	if err != nil {
		t.Fatalf("ensure packing: %v", err)
	}
	if p.ID == b1.ID {
		t.Fatal("kinds must get distinct lists")
	}
	// The seeded activities list must survive alongside the new kinds.
	acts, err := s.ActivitiesList(tripID)
	if err != nil || acts.Kind != "activity" {
		t.Fatalf("activities list: %v (kind %q)", err, acts.Kind)
	}
}

func TestSetListItemMetaMergesAndDeletes(t *testing.T) {
	s := newTestStore(t)
	tripID, _ := s.CreateTrip("Trip", nil)
	list, _ := s.EnsureList(tripID, "booking", "Bookings")
	id, err := s.AddListItem(list.ID, "Flights", "", "", map[string]any{"status": "todo", "cost": "1200"}, nil)
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := s.SetListItemMeta(id, map[string]any{"status": "booked", "ref": "ABC123", "cost": nil}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	got, _ := s.EnsureList(tripID, "booking", "Bookings")
	if len(got.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(got.Items))
	}
	m := got.Items[0].Meta
	if m["status"] != "booked" || m["ref"] != "ABC123" {
		t.Fatalf("unexpected meta after merge: %v", m)
	}
	if _, ok := m["cost"]; ok {
		t.Fatalf("nil value should delete the key, got %v", m)
	}

	if err := s.SetListItemMeta(9999, map[string]any{"x": "y"}); err != ErrNotFound {
		t.Fatalf("want ErrNotFound for missing item, got %v", err)
	}
}
