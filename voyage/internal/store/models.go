package store

// This file holds the in-memory shapes the store returns. DB-backed fields are
// populated by the store; fields tagged "computed" are filled by callers (the
// server) for rendering — e.g. vote tallies and the current voter's selections.

// Trip is a holiday being planned.
type Trip struct {
	ID         int64
	Title      string
	Stage      string
	ShareToken string
	Notes      string
	PartySize  int // number of travellers, for "per person" totals
	CreatedAt  string
	UpdatedAt  string
}

// TripSummary is a lightweight row for the dashboard list.
type TripSummary struct {
	ID         int64
	Title      string
	Stage      string
	CreatedAt  string
	Scenarios  int // combos that have at least one item
	Activities int
}

// Location is a place visited on a trip.
type Location struct {
	ID       int64
	TripID   int64
	Name     string
	Position int
	Arrive   string
	Depart   string
	Notes    string
}

// Axis is a dimension of variation for a trip (budget, dates, or generic).
type Axis struct {
	ID       int64
	TripID   int64
	Name     string
	Kind     string
	Position int
	Options  []AxisOption
}

// AxisOption is one alternative value along an axis (a budget or a date range).
type AxisOption struct {
	ID         int64
	AxisID     int64
	Label      string
	Position   int
	Meta       map[string]any
	Votes      int    // computed
	Voted      bool   // computed
	Nights     int    // computed (date ranges): nights between start and end
	TotalLabel string // computed: e.g. "≈ $5,000 for 2" for a per-person budget
}

// ComboOption ties a combo to one option of one axis (one row per axis).
type ComboOption struct {
	AxisID       int64
	AxisName     string
	AxisKind     string
	AxisOptionID int64
	OptionLabel  string
}

// Combo is a tuple of axis options (e.g. a budget × dates pairing) that hotel
// options hang off. v1 combos always span the budget and dates axes.
type Combo struct {
	ID       int64
	TripID   int64
	Label    string
	Position int
	Status   string
	Options  []ComboOption
	Items    []ComboItem
	Votes    int  // computed
	Voted    bool // computed
}

// Summary renders a combo's option labels as "Mid budget · 12–19 Jul".
func (c Combo) Summary() string {
	if c.Label != "" {
		return c.Label
	}
	out := ""
	for i, o := range c.Options {
		if i > 0 {
			out += " · "
		}
		out += o.OptionLabel
	}
	if out == "" {
		return "Untitled scenario"
	}
	return out
}

// ComboItem is a dependent option attached to a combo — a hotel in v1, but the
// category column generalises to flights, transport, etc.
type ComboItem struct {
	ID         int64
	ComboID    int64
	Category   string
	Label      string
	Position   int
	Status     string
	Link       string
	Notes      string
	Meta       map[string]any
	Votes      int    // computed
	Voted      bool   // computed
	TotalLabel string // computed: e.g. "≈ $1,800 · 9 nts" for a per-night stay
}

// List is a generic rankable list belonging to a trip (Activities in v1).
type List struct {
	ID       int64
	TripID   int64
	Name     string
	Kind     string
	Position int
	Items    []ListItem
}

// ListItem is one entry in a list. Position is the manual rank.
type ListItem struct {
	ID         int64
	ListID     int64
	Label      string
	Position   int
	Notes      string
	Link       string
	LocationID int64 // 0 when unset
	Meta       map[string]any
	Votes      int  // computed
	Voted      bool // computed
}

// Voter is a fellow traveller identified per-trip by a cookie token.
type Voter struct {
	ID        int64
	TripID    int64
	Name      string
	Token     string
	CreatedAt string
}
