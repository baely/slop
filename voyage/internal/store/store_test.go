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

	bOpt, _ := s.AddAxisOption(budget.ID, "£2500 pp", nil)
	dOpt, _ := s.AddAxisOption(dates.ID, "12–19 Jul", nil)
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
	dOpt2, _ := s.AddAxisOption(dates.ID, "5–12 Sep", nil)
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
	bOpt, _ := s.AddAxisOption(budget.ID, "mid", nil)
	dOpt, _ := s.AddAxisOption(dates.ID, "summer", nil)
	combo, _ := s.EnsureCombo(tripID, map[int64]int64{budget.ID: bOpt, dates.ID: dOpt})

	h1, _ := s.AddComboItem(combo, "hotel", "One", "", "", nil)
	h2, _ := s.AddComboItem(combo, "hotel", "Two", "", "", nil)
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
