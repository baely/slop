// Package server wires Voyage's HTTP layer: an owner-facing planner (token
// gated) and a public, share-token view where fellow travellers vote on options.
package server

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/baileybutler/voyage/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

// Options configures the server.
type Options struct {
	Store      *store.Store
	Title      string
	AdminToken string // when empty, owner auth is disabled (dev only)
	BaseURL    string // e.g. https://voyage.baileys.app, for absolute share links
	Currency   string // default currency code prefilled in forms (e.g. AUD)
	TrustProxy bool
}

// Server is the HTTP handler for Voyage.
type Server struct {
	opts  Options
	store *store.Store
	mux   *http.ServeMux
	tpl   *template.Template
}

// New builds a Server with routes and templates ready to serve.
func New(opts Options) *Server {
	if opts.Title == "" {
		opts.Title = "Voyage"
	}
	if opts.Currency == "" {
		opts.Currency = "AUD"
	}
	s := &Server{
		opts:  opts,
		store: opts.Store,
		mux:   http.NewServeMux(),
		tpl:   template.Must(template.New("").Funcs(funcMap(opts.Currency)).ParseFS(templateFS, "templates/*.html")),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })

	// Auth.
	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)

	// Owner (token gated).
	s.mux.Handle("GET /{$}", s.auth(s.handleDashboard))
	s.mux.Handle("POST /trips", s.auth(s.handleCreateTrip))
	s.mux.Handle("POST /trips/{id}/delete", s.auth(s.handleDeleteTrip))
	s.mux.Handle("GET /trips/{id}", s.auth(s.handleTrip))
	s.mux.Handle("POST /trips/{id}/locations", s.auth(s.handleAddLocation))
	s.mux.Handle("POST /locations/{id}/delete", s.auth(s.handleDeleteLocation))
	s.mux.Handle("POST /axes/{id}/options", s.auth(s.handleAddOption))
	s.mux.Handle("POST /options/{id}/delete", s.auth(s.handleDeleteOption))
	s.mux.Handle("POST /options/{id}/hotels", s.auth(s.handleAddBudgetHotel))
	s.mux.Handle("POST /items/{id}/delete", s.auth(s.handleDeleteItem))
	s.mux.Handle("POST /items/{id}/status", s.auth(s.handleItemStatus))
	s.mux.Handle("POST /trips/{id}/activities", s.auth(s.handleAddActivity))
	s.mux.Handle("POST /activities/{id}/delete", s.auth(s.handleDeleteActivity))
	s.mux.Handle("POST /activities/{id}/move", s.auth(s.handleMoveActivity))
	s.mux.Handle("POST /trips/{id}/party", s.auth(s.handleSetParty))
	s.mux.Handle("POST /trips/{id}/share/rotate", s.auth(s.handleRotateShare))
	s.mux.Handle("POST /trips/{id}/comment", s.auth(s.handleOwnerComment))
	s.mux.Handle("POST /comments/{id}/delete", s.auth(s.handleOwnerDeleteComment))

	// Traveller (share token, no login).
	s.mux.HandleFunc("GET /t/{token}", s.handleShare)
	s.mux.HandleFunc("POST /t/{token}/identify", s.handleIdentify)
	s.mux.HandleFunc("POST /t/{token}/vote", s.handleVote)
	s.mux.HandleFunc("POST /t/{token}/rank", s.handleRank)
	s.mux.HandleFunc("POST /t/{token}/comment", s.handleShareComment)
	s.mux.HandleFunc("POST /t/{token}/comment/{id}/delete", s.handleShareDeleteComment)
}

// ---- rendering helpers ----

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func funcMap(defaultCurrency string) template.FuncMap {
	return template.FuncMap{
		"commentsFor": func(m map[string][]store.Comment, t string, id int64) []store.Comment {
			return m[fmt.Sprintf("%s:%d", t, id)]
		},
		"metaStr": func(m map[string]any, k string) string {
			if m == nil {
				return ""
			}
			if v, ok := m[k]; ok {
				return fmt.Sprintf("%v", v)
			}
			return ""
		},
		// cur turns a currency code into its symbol for display ("AUD" -> "$").
		"cur": func(code string) string {
			if code == "" {
				return ""
			}
			return currencySymbol(code)
		},
		// defaultCurrency is the code prefilled in new option/hotel forms.
		"defaultCurrency": func() string { return defaultCurrency },
		"add":             func(a, b int) int { return a + b },
		// dict builds a map for passing multiple values into a sub-template.
		"dict": func(kv ...any) map[string]any {
			m := make(map[string]any, len(kv)/2)
			for i := 0; i+1 < len(kv); i += 2 {
				if k, ok := kv[i].(string); ok {
					m[k] = kv[i+1]
				}
			}
			return m
		},
	}
}

func key(t string, id int64) string { return fmt.Sprintf("%s:%d", t, id) }

func parseID(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id
}

func formInt(r *http.Request, name string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue(name)), 10, 64)
	return id
}

// redirectBack returns to the form's redirect field, else the Referer, else
// fallback. POST handlers must have called ParseForm (FormValue does so).
func redirectBack(w http.ResponseWriter, r *http.Request, fallback string) {
	dest := r.FormValue("redirect")
	if dest == "" {
		dest = r.Referer()
	}
	if dest == "" {
		dest = fallback
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (s *Server) shareURL(r *http.Request, token string) string {
	base := strings.TrimRight(s.opts.BaseURL, "/")
	if base == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/t/" + token
}

// ---- owner: dashboard & trips ----

type dashboardPage struct {
	Title string
	Trips []store.TripSummary
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	trips, err := s.store.ListTrips()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "dashboard", dashboardPage{Title: s.opts.Title, Trips: trips})
}

func (s *Server) handleCreateTrip(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		redirectBack(w, r, "/")
		return
	}
	var locs []string
	for _, line := range strings.Split(r.FormValue("locations"), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			locs = append(locs, t)
		}
	}
	id, err := s.store.CreateTrip(title, locs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/trips/%d?mode=ideate", id), http.StatusSeeOther)
}

func (s *Server) handleDeleteTrip(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTrip(parseID(r, "id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- owner: trip workspace ----

// budgetGroup is a budget option together with the hotel options grouped under
// it. Hotels are grouped by budget only (dates are an independent votable list).
type budgetGroup struct {
	Option store.AxisOption
	Hotels []store.ComboItem
}

type tripPage struct {
	Title           string
	Trip            *store.Trip
	Mode            string
	Locations       []store.Location
	BudgetAxis      store.Axis
	DatesAxis       store.Axis
	Budgets         []budgetGroup
	Dates           []store.AxisOption
	Activities      *store.List
	Comments        map[string][]store.Comment
	ShareURL        string
	CurrentURL      string
	TripNights      int
	TripNightsLabel string
}

func (s *Server) handleTrip(w http.ResponseWriter, r *http.Request) {
	id := parseID(r, "id")
	trip, err := s.store.GetTrip(id)
	if err == store.ErrNotFound {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode != "plan" {
		mode = "ideate"
	}

	locations, _ := s.store.LocationsForTrip(id)
	axes, _ := s.store.AxesForTrip(id)
	combos, _ := s.store.CombosForTrip(id)
	counts, _ := s.store.VoteCounts(id)
	comments, _ := s.store.CommentsFor(id)
	activities, _ := s.store.ActivitiesList(id)

	var budget, dates store.Axis
	for _, a := range axes {
		switch a.Kind {
		case "budget":
			budget = a
		case "date_range":
			dates = a
		}
	}

	dateOpts := attachOptionVotes(dates.Options, counts, nil)
	nights, nightsLabel := referenceNights(dateOpts)
	budgets := s.budgetGroups(budget, combos, counts, nil, trip.PartySize, nights)

	// In Plan, activities show the group's weighted ranking (summed traveller
	// points) with each activity's score attached. In Ideate they stay in the
	// organiser's manual order so the up/down arrows make sense.
	if activities != nil {
		if mode == "plan" {
			activities.Items = activityOrder(activities.Items, nil, counts)
		}
		for i := range activities.Items {
			activities.Items[i].Votes = counts[key("list_item", activities.Items[i].ID)]
		}
	}

	s.render(w, "trip", tripPage{
		Title:           trip.Title + " · " + s.opts.Title,
		Trip:            trip,
		Mode:            mode,
		Locations:       locations,
		BudgetAxis:      budget,
		DatesAxis:       dates,
		Budgets:         budgets,
		Dates:           dateOpts,
		Activities:      activities,
		Comments:        comments,
		ShareURL:        s.shareURL(r, trip.ShareToken),
		CurrentURL:      r.URL.String(),
		TripNights:      nights,
		TripNightsLabel: nightsLabel,
	})
}

// budgetGroups pairs each budget option with the hotels grouped under it (via a
// budget-only combo), attaching vote tallies (and the voter's selections when
// voted is non-nil).
func (s *Server) budgetGroups(budget store.Axis, combos []store.Combo, counts map[string]int, voted map[string]bool, party, nights int) []budgetGroup {
	// Index budget-only combos by their budget option id.
	byBudgetOpt := map[int64]*store.Combo{}
	for i := range combos {
		for _, o := range combos[i].Options {
			if o.AxisKind == "budget" {
				byBudgetOpt[o.AxisOptionID] = &combos[i]
			}
		}
	}

	groups := make([]budgetGroup, 0, len(budget.Options))
	for _, opt := range budget.Options {
		opt.Votes = counts[key("axis_option", opt.ID)]
		if voted != nil {
			opt.Voted = voted[key("axis_option", opt.ID)]
		}
		opt.TotalLabel = budgetTotalLabel(opt.Meta, party)
		var hotels []store.ComboItem
		if c := byBudgetOpt[opt.ID]; c != nil {
			hotels = c.Items
			for j := range hotels {
				hotels[j].Votes = counts[key("combo_item", hotels[j].ID)]
				if voted != nil {
					hotels[j].Voted = voted[key("combo_item", hotels[j].ID)]
				}
				hotels[j].TotalLabel = accomTotalLabel(hotels[j].Meta, nights)
			}
		}
		groups = append(groups, budgetGroup{Option: opt, Hotels: hotels})
	}
	return groups
}

// attachOptionVotes returns axis options with vote tallies (and the voter's
// selections when voted is non-nil) and computed nights (for date ranges).
func attachOptionVotes(opts []store.AxisOption, counts map[string]int, voted map[string]bool) []store.AxisOption {
	out := make([]store.AxisOption, 0, len(opts))
	for _, o := range opts {
		o.Votes = counts[key("axis_option", o.ID)]
		if voted != nil {
			o.Voted = voted[key("axis_option", o.ID)]
		}
		o.Nights = nightsBetween(metaString(o.Meta, "start"), metaString(o.Meta, "end"))
		out = append(out, o)
	}
	return out
}

// ---- owner: locations, axes/options ----

func (s *Server) handleAddLocation(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	name := strings.TrimSpace(r.FormValue("name"))
	if name != "" {
		_ = s.store.AddLocation(tripID, name)
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d", tripID))
}

func (s *Server) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	_ = s.store.DeleteLocation(parseID(r, "id"))
	redirectBack(w, r, "/")
}

func (s *Server) handleAddOption(w http.ResponseWriter, r *http.Request) {
	axisID := parseID(r, "id")
	axis, err := s.store.AxisByID(axisID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	meta := map[string]any{}
	switch axis.Kind {
	case "budget":
		amount := strings.TrimSpace(r.FormValue("amount"))
		currency := strings.TrimSpace(r.FormValue("currency"))
		basis := strings.TrimSpace(r.FormValue("basis"))
		if amount != "" {
			meta["amount"] = amount
		}
		if currency != "" {
			meta["currency"] = currency
		}
		if basis != "" {
			meta["basis"] = basis
		}
		if label == "" {
			label = budgetLabel(amount, currency, basis)
		}
	case "date_range":
		start := strings.TrimSpace(r.FormValue("start"))
		end := strings.TrimSpace(r.FormValue("end"))
		if start != "" {
			meta["start"] = start
		}
		if end != "" {
			meta["end"] = end
		}
		if label == "" {
			label = formatDateRange(start, end)
		}
	}
	if label != "" {
		_, _ = s.store.AddAxisOption(axisID, label, meta)
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", axis.TripID))
}

func (s *Server) handleDeleteOption(w http.ResponseWriter, r *http.Request) {
	optID := parseID(r, "id")
	tripID, _ := s.store.AxisOptionTripID(optID)
	_ = s.store.DeleteAxisOption(optID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

// ---- owner: combos & items ----

// handleAddBudgetHotel adds a hotel option grouped under a budget option. Hotels
// hang off a budget-only combo, created on demand.
func (s *Server) handleAddBudgetHotel(w http.ResponseWriter, r *http.Request) {
	optID := parseID(r, "id")
	axisID, err := s.store.AxisOptionAxisID(optID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tripID, _ := s.store.AxisOptionTripID(optID)
	label := strings.TrimSpace(r.FormValue("label"))
	if label != "" {
		comboID, err := s.store.EnsureCombo(tripID, map[int64]int64{axisID: optID})
		if err == nil {
			meta := map[string]any{}
			for _, k := range []string{"price", "currency", "basis", "area", "rating"} {
				if v := strings.TrimSpace(r.FormValue(k)); v != "" {
					meta[k] = v
				}
			}
			_, _ = s.store.AddComboItem(comboID, "hotel", label,
				strings.TrimSpace(r.FormValue("link")), strings.TrimSpace(r.FormValue("notes")), meta)
		}
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	comboID, _ := s.store.ComboItemComboID(itemID)
	_ = s.store.DeleteComboItem(itemID)
	tripID, _ := s.store.ComboTripID(comboID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

func (s *Server) handleItemStatus(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	_ = s.store.SetComboItemStatus(itemID, strings.TrimSpace(r.FormValue("status")))
	comboID, _ := s.store.ComboItemComboID(itemID)
	tripID, _ := s.store.ComboTripID(comboID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=plan&combo=%d", tripID, comboID))
}

// ---- owner: activities ----

func (s *Server) handleAddActivity(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	label := strings.TrimSpace(r.FormValue("label"))
	if label != "" {
		if list, err := s.store.ActivitiesList(tripID); err == nil {
			_, _ = s.store.AddListItem(list.ID, label, strings.TrimSpace(r.FormValue("notes")), strings.TrimSpace(r.FormValue("link")))
		}
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

func (s *Server) handleDeleteActivity(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	tripID, _ := s.store.ListItemTripID(itemID)
	_ = s.store.DeleteListItem(itemID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

func (s *Server) handleMoveActivity(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	tripID, _ := s.store.ListItemTripID(itemID)
	_ = s.store.MoveListItem(itemID, strings.TrimSpace(r.FormValue("dir")))
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

// ---- owner: sharing & comments ----

func (s *Server) handleSetParty(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	if n := int(formInt(r, "size")); n > 0 {
		_ = s.store.SetPartySize(tripID, n)
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=ideate", tripID))
}

func (s *Server) handleRotateShare(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	_, _ = s.store.RotateShareToken(tripID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=plan", tripID))
}

func (s *Server) handleOwnerComment(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	body := strings.TrimSpace(r.FormValue("body"))
	if body != "" {
		_ = s.store.AddComment(tripID, nil, strings.TrimSpace(r.FormValue("target_type")), formInt(r, "target_id"), body)
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=plan", tripID))
}

func (s *Server) handleOwnerDeleteComment(w http.ResponseWriter, r *http.Request) {
	commentID := parseID(r, "id")
	tripID, _ := s.store.CommentTripID(commentID)
	_ = s.store.DeleteComment(commentID, nil) // owner deletes only their own (NULL-author) comments
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=plan", tripID))
}

// ---- label helpers ----

func budgetLabel(amount, currency, basis string) string {
	if amount == "" {
		return ""
	}
	out := currencySymbol(currency) + amount
	if strings.EqualFold(basis, "pp") {
		out += " pp"
	}
	return strings.TrimSpace(out)
}

// currencySymbol maps a currency code to its display symbol, falling back to the
// code itself for anything unmapped.
func currencySymbol(code string) string {
	switch strings.ToUpper(code) {
	case "GBP":
		return "£"
	case "EUR":
		return "€"
	case "JPY", "CNY":
		return "¥"
	case "USD", "AUD", "CAD", "NZD", "SGD", "HKD":
		return "$"
	default:
		return code
	}
}

func formatDateRange(start, end string) string {
	const in = "2006-01-02"
	ps, es := parseDate(start, in), parseDate(end, in)
	switch {
	case ps != nil && es != nil:
		if ps.Year() == es.Year() {
			return ps.Format("2 Jan") + " – " + es.Format("2 Jan 2006")
		}
		return ps.Format("2 Jan 2006") + " – " + es.Format("2 Jan 2006")
	case ps != nil:
		return "From " + ps.Format("2 Jan 2006")
	case es != nil:
		return "Until " + es.Format("2 Jan 2006")
	default:
		return ""
	}
}

func parseDate(s, layout string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return nil
	}
	return &t
}

// ---- cost totals ----

func metaString(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// parseAmount reads a user-entered money figure, tolerating commas/spaces/symbols.
func parseAmount(s string) (float64, bool) {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// commaInt renders an integer with thousands separators (e.g. 12000 -> "12,000").
func commaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

func formatMoney(currency string, v float64) string {
	return currencySymbol(currency) + commaInt(int64(v+0.5))
}

// nightsBetween returns the whole nights between two YYYY-MM-DD dates, or 0.
func nightsBetween(start, end string) int {
	const layout = "2006-01-02"
	ps, es := parseDate(start, layout), parseDate(end, layout)
	if ps == nil || es == nil {
		return 0
	}
	n := int(es.Sub(*ps).Hours()/24 + 0.5)
	if n < 0 {
		return 0
	}
	return n
}

// budgetTotalLabel turns a per-person budget into a group total for `party`.
func budgetTotalLabel(meta map[string]any, party int) string {
	if party < 1 {
		party = 1
	}
	if !strings.EqualFold(metaString(meta, "basis"), "pp") {
		return ""
	}
	amt, ok := parseAmount(metaString(meta, "amount"))
	if !ok {
		return ""
	}
	people := "people"
	if party == 1 {
		people = "person"
	}
	return "≈ " + formatMoney(metaString(meta, "currency"), amt*float64(party)) + " for " + strconv.Itoa(party) + " " + people
}

// accomTotalLabel turns a per-night stay into a stay total for `nights`.
func accomTotalLabel(meta map[string]any, nights int) string {
	if nights <= 0 || !strings.EqualFold(metaString(meta, "basis"), "night") {
		return ""
	}
	price, ok := parseAmount(metaString(meta, "price"))
	if !ok {
		return ""
	}
	return "≈ " + formatMoney(metaString(meta, "currency"), price*float64(nights)) + " · " + strconv.Itoa(nights) + " nts"
}

// referenceNights picks the nights figure used for accommodation totals: the
// most-voted date range, breaking ties by the longest. Returns 0 if none.
func referenceNights(dates []store.AxisOption) (int, string) {
	best := -1
	var nights int
	var label string
	for _, d := range dates {
		if d.Nights <= 0 {
			continue
		}
		score := d.Votes
		if score > best || (score == best && d.Nights > nights) {
			best = score
			nights = d.Nights
			label = d.Label
		}
	}
	return nights, label
}
