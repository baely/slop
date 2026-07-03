// The Book and Travel stages: bookings with a status flow and budget tracking,
// a day-by-day itinerary derived from the locked-in dates, and a packing
// checklist. Bookings, itinerary entries and packing items are all list_items,
// so they ride the existing lists spine with kind-specific metadata.
package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/baileybutler/voyage/internal/store"
)

// tripStages are the lifecycle stages a trip moves through, in order.
var tripStages = []string{"ideate", "plan", "book", "travel"}

// bookingCategories orders the Book screen's groups.
var bookingCategories = []struct{ Key, Label string }{
	{"flight", "Flights"},
	{"stay", "Stay"},
	{"transfer", "Transfers"},
	{"activity", "Activities"},
	{"other", "Other"},
}

// itinerarySlots orders entries within a day.
var itinerarySlots = []string{"", "morning", "afternoon", "evening"}

func slotRank(slot string) int {
	for i, s := range itinerarySlots {
		if s == slot {
			return i
		}
	}
	return len(itinerarySlots)
}

// ---- Book: data ----

type bookGroup struct {
	Key   string
	Label string
	Items []store.ListItem
}

type bookData struct {
	Groups       []bookGroup
	HasItems     bool
	PlannedLabel string // every booking with a cost
	BookedLabel  string // bookings actually booked/paid
	BudgetLabel  string // target from the locked budget
	BudgetPct    int    // booked as % of target, capped at 100
	ImportItem   *store.ComboItem
	ImportLabel  string // cost preview for the importable stay
}

// buildBookData groups the trip's bookings by category and totals them against
// the locked budget.
func buildBookData(trip *store.Trip, list *store.List, lockedBudget *store.AxisOption, combos []store.Combo, nights int) *bookData {
	d := &bookData{}
	if list == nil {
		return d
	}

	byCat := map[string][]store.ListItem{}
	var planned, booked float64
	// Totals are compared against the locked budget, so borrow its currency;
	// costs in other currencies are summed as-is (close enough for planning).
	currency := ""
	if lockedBudget != nil {
		currency = metaString(lockedBudget.Meta, "currency")
	}
	for _, it := range list.Items {
		cat := metaString(it.Meta, "category")
		if _, ok := byCat[cat]; !ok && !knownCategory(cat) {
			cat = "other"
		}
		byCat[cat] = append(byCat[cat], it)
		d.HasItems = true
		if cost, ok := parseAmount(metaString(it.Meta, "cost")); ok {
			planned += cost
			if st := metaString(it.Meta, "status"); st == "booked" || st == "paid" {
				booked += cost
			}
			if currency == "" {
				currency = metaString(it.Meta, "currency")
			}
		}
	}
	for _, c := range bookingCategories {
		if items := byCat[c.Key]; len(items) > 0 {
			d.Groups = append(d.Groups, bookGroup{Key: c.Key, Label: c.Label, Items: items})
		}
	}

	if planned > 0 {
		d.PlannedLabel = formatMoney(currency, planned)
	}
	d.BookedLabel = formatMoney(currency, booked)
	if lockedBudget != nil {
		if amt, ok := parseAmount(metaString(lockedBudget.Meta, "amount")); ok {
			party := trip.PartySize
			if party < 1 {
				party = 1
			}
			if strings.EqualFold(metaString(lockedBudget.Meta, "basis"), "pp") {
				amt *= float64(party)
			}
			d.BudgetLabel = formatMoney(metaString(lockedBudget.Meta, "currency"), amt)
			if amt > 0 {
				pct := int(booked/amt*100 + 0.5)
				if pct > 100 {
					pct = 100
				}
				d.BudgetPct = pct
			}
		}
	}

	// Offer to pull the chosen stay across as a booking, once.
	if item := chosenStay(combos); item != nil && !bookingImported(list.Items, item.ID) {
		d.ImportItem = item
		if cost, ok := stayCost(item.Meta, nights); ok {
			d.ImportLabel = formatMoney(metaString(item.Meta, "currency"), cost)
		}
	}
	return d
}

func knownCategory(cat string) bool {
	for _, c := range bookingCategories {
		if c.Key == cat {
			return true
		}
	}
	return false
}

// chosenStay returns the accommodation the group settled on: a selected item
// first, else a preferred one.
func chosenStay(combos []store.Combo) *store.ComboItem {
	var preferred *store.ComboItem
	for i := range combos {
		for j := range combos[i].Items {
			it := combos[i].Items[j]
			switch it.Status {
			case "selected":
				return &it
			case "preferred":
				if preferred == nil {
					preferred = &it
				}
			}
		}
	}
	return preferred
}

// stayCost turns a stay's price meta into a bookable total: per-night prices
// are multiplied out by the trip's nights.
func stayCost(meta map[string]any, nights int) (float64, bool) {
	price, ok := parseAmount(metaString(meta, "price"))
	if !ok {
		return 0, false
	}
	if strings.EqualFold(metaString(meta, "basis"), "night") {
		if nights <= 0 {
			return 0, false
		}
		return price * float64(nights), true
	}
	return price, true
}

// bookingImported reports whether a booking was already created from the given
// combo item (so the import button disappears after use).
func bookingImported(items []store.ListItem, itemID int64) bool {
	want := strconv.FormatInt(itemID, 10)
	for _, it := range items {
		if metaString(it.Meta, "source") == want {
			return true
		}
	}
	return false
}

// ---- Book: handlers ----

func (s *Server) handleAddBooking(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	label := strings.TrimSpace(r.FormValue("label"))
	if label != "" {
		meta := map[string]any{"status": "todo"}
		cat := strings.TrimSpace(r.FormValue("category"))
		if !knownCategory(cat) {
			cat = "other"
		}
		meta["category"] = cat
		for _, k := range []string{"cost", "currency", "ref", "date"} {
			if v := strings.TrimSpace(r.FormValue(k)); v != "" {
				meta[k] = v
			}
		}
		if list, err := s.store.EnsureList(tripID, "booking", "Bookings"); err == nil {
			_, _ = s.store.AddListItem(list.ID, label, strings.TrimSpace(r.FormValue("notes")), strings.TrimSpace(r.FormValue("link")), meta, nil)
		}
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=book", tripID))
}

func (s *Server) handleBookingStatus(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	status := strings.TrimSpace(r.FormValue("status"))
	if status == "todo" || status == "booked" || status == "paid" {
		_ = s.store.SetListItemMeta(itemID, map[string]any{"status": status})
	}
	tripID, _ := s.store.ListItemTripID(itemID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=book", tripID))
}

// handleImportStay copies the chosen accommodation into the bookings list.
func (s *Server) handleImportStay(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	combos, _ := s.store.CombosForTrip(tripID)
	item := chosenStay(combos)
	if item == nil {
		redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=book", tripID))
		return
	}
	list, err := s.store.EnsureList(tripID, "booking", "Bookings")
	if err == nil && !bookingImported(list.Items, item.ID) {
		meta := map[string]any{
			"category": "stay",
			"status":   "todo",
			"source":   strconv.FormatInt(item.ID, 10),
		}
		if cur := metaString(item.Meta, "currency"); cur != "" {
			meta["currency"] = cur
		}
		axes, _ := s.store.AxesForTrip(tripID)
		counts, _ := s.store.VoteCounts(tripID)
		for _, a := range axes {
			if a.Kind == "date_range" {
				nights, _ := referenceNights(attachOptionVotes(a.Options, counts, nil))
				if cost, ok := stayCost(item.Meta, nights); ok {
					meta["cost"] = strconv.FormatInt(int64(cost+0.5), 10)
				}
			}
		}
		notes := item.Notes
		if area := metaString(item.Meta, "area"); area != "" && notes == "" {
			notes = area
		}
		_, _ = s.store.AddListItem(list.ID, item.Label, notes, item.Link, meta, nil)
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=book", tripID))
}

// ---- Travel: data ----

type itinDay struct {
	Date    string // YYYY-MM-DD
	Label   string // "Mon 14 Sep"
	Num     int    // Day 1..N
	Entries []store.ListItem
}

type travelData struct {
	HasDates    bool
	DatesLabel  string
	Countdown   string
	Days        []itinDay
	Unscheduled []store.ListItem // itinerary entries without a day in range
	Wishlist    []store.ListItem // ranked activities not yet scheduled
	Packing     *store.List
	PackedCount int
}

// maxItineraryDays caps how many day cards are rendered for very long ranges.
const maxItineraryDays = 60

// buildTravelData lays the itinerary out over the locked date range and works
// out the countdown, the unscheduled wishlist, and packing progress.
func buildTravelData(lockedDates *store.AxisOption, itin, packing *store.List, rankedActivities []store.ListItem, now time.Time) *travelData {
	d := &travelData{Packing: packing}

	var start, end *time.Time
	if lockedDates != nil {
		d.DatesLabel = lockedDates.Label
		start = parseDate(metaString(lockedDates.Meta, "start"), "2006-01-02")
		end = parseDate(metaString(lockedDates.Meta, "end"), "2006-01-02")
	}
	if start != nil && end != nil && !end.Before(*start) {
		d.HasDates = true
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		totalDays := int(end.Sub(*start).Hours()/24) + 1
		switch {
		case today.Before(*start):
			n := int(start.Sub(today).Hours() / 24)
			if n == 1 {
				d.Countdown = "1 day to go"
			} else {
				d.Countdown = fmt.Sprintf("%d days to go", n)
			}
		case !today.After(*end):
			d.Countdown = fmt.Sprintf("Day %d of %d", int(today.Sub(*start).Hours()/24)+1, totalDays)
		default:
			d.Countdown = "Trip complete"
		}

		days := totalDays
		if days > maxItineraryDays {
			days = maxItineraryDays
		}
		for i := 0; i < days; i++ {
			dt := start.AddDate(0, 0, i)
			d.Days = append(d.Days, itinDay{
				Date:  dt.Format("2006-01-02"),
				Label: dt.Format("Mon 2 Jan"),
				Num:   i + 1,
			})
		}
	}

	// Slot itinerary entries into their days; anything else is unscheduled.
	scheduled := map[string]bool{} // activity ids already on the itinerary
	if itin != nil {
		byDate := map[string]int{}
		for i := range d.Days {
			byDate[d.Days[i].Date] = i
		}
		for _, it := range itin.Items {
			if src := metaString(it.Meta, "activity"); src != "" {
				scheduled[src] = true
			}
			if i, ok := byDate[metaString(it.Meta, "day")]; ok {
				d.Days[i].Entries = append(d.Days[i].Entries, it)
			} else {
				d.Unscheduled = append(d.Unscheduled, it)
			}
		}
		for i := range d.Days {
			sortEntriesBySlot(d.Days[i].Entries)
		}
	}

	// The group's ranked wishlist, minus what's already scheduled, feeds the
	// "add to a day" shortcuts.
	for _, a := range rankedActivities {
		if !scheduled[strconv.FormatInt(a.ID, 10)] {
			d.Wishlist = append(d.Wishlist, a)
		}
	}

	if packing != nil {
		for _, it := range packing.Items {
			if metaString(it.Meta, "done") == "1" {
				d.PackedCount++
			}
		}
	}
	return d
}

func sortEntriesBySlot(entries []store.ListItem) {
	// Stable insertion sort — day entries are few and already position-ordered.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && slotRank(metaString(entries[j].Meta, "slot")) < slotRank(metaString(entries[j-1].Meta, "slot")); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// ---- Travel: handlers ----

// handleAddItinerary adds an itinerary entry — freeform, or scheduled straight
// from the activity wishlist via activity=<id>.
func (s *Server) handleAddItinerary(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	meta := map[string]any{}
	if day := strings.TrimSpace(r.FormValue("day")); day != "" {
		meta["day"] = day
	}
	if slot := strings.TrimSpace(r.FormValue("slot")); slot != "" && slotRank(slot) < len(itinerarySlots) {
		meta["slot"] = slot
	}

	label := strings.TrimSpace(r.FormValue("label"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	link := strings.TrimSpace(r.FormValue("link"))
	if actID := formInt(r, "activity"); actID != 0 {
		if acts, err := s.store.ActivitiesList(tripID); err == nil {
			for _, a := range acts.Items {
				if a.ID == actID {
					label, notes, link = a.Label, a.Notes, a.Link
					meta["activity"] = strconv.FormatInt(a.ID, 10)
					break
				}
			}
		}
	}
	if label != "" {
		if list, err := s.store.EnsureList(tripID, "itinerary", "Itinerary"); err == nil {
			_, _ = s.store.AddListItem(list.ID, label, notes, link, meta, nil)
		}
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=travel", tripID))
}

// handleItineraryDay reschedules an entry to another day/slot.
func (s *Server) handleItineraryDay(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	patch := map[string]any{}
	if day := strings.TrimSpace(r.FormValue("day")); day != "" {
		patch["day"] = day
	}
	if slot := strings.TrimSpace(r.FormValue("slot")); slotRank(slot) < len(itinerarySlots) {
		if slot == "" {
			patch["slot"] = nil
		} else {
			patch["slot"] = slot
		}
	}
	if len(patch) > 0 {
		_ = s.store.SetListItemMeta(itemID, patch)
	}
	tripID, _ := s.store.ListItemTripID(itemID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=travel", tripID))
}

func (s *Server) handleAddPacking(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	if label := strings.TrimSpace(r.FormValue("label")); label != "" {
		if list, err := s.store.EnsureList(tripID, "packing", "Packing"); err == nil {
			_, _ = s.store.AddListItem(list.ID, label, "", "", nil, nil)
		}
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=travel", tripID))
}

func (s *Server) handlePackingToggle(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	patch := map[string]any{"done": nil}
	if r.FormValue("done") == "1" {
		patch["done"] = "1"
	}
	_ = s.store.SetListItemMeta(itemID, patch)
	tripID, _ := s.store.ListItemTripID(itemID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=travel", tripID))
}

// ---- shared: stage, locking, deletes ----

func (s *Server) handleSetStage(w http.ResponseWriter, r *http.Request) {
	tripID := parseID(r, "id")
	stage := strings.TrimSpace(r.FormValue("stage"))
	for _, st := range tripStages {
		if st == stage {
			_ = s.store.SetStage(tripID, stage)
			break
		}
	}
	redirectBack(w, r, fmt.Sprintf("/trips/%d", tripID))
}

// handleOptionStatus locks in (or unlocks) a budget or date-range option.
func (s *Server) handleOptionStatus(w http.ResponseWriter, r *http.Request) {
	optID := parseID(r, "id")
	status := strings.TrimSpace(r.FormValue("status"))
	if status == "selected" || status == "option" {
		_ = s.store.SetAxisOptionStatus(optID, status)
	}
	tripID, _ := s.store.AxisOptionTripID(optID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d?mode=plan", tripID))
}

// handleDeleteListItem deletes any list-spine item (booking, itinerary entry,
// packing item).
func (s *Server) handleDeleteListItem(w http.ResponseWriter, r *http.Request) {
	itemID := parseID(r, "id")
	tripID, _ := s.store.ListItemTripID(itemID)
	_ = s.store.DeleteListItem(itemID)
	redirectBack(w, r, fmt.Sprintf("/trips/%d", tripID))
}

// ---- share: the read-only plan summary ----

type planSummary struct {
	DatesLabel string
	Countdown  string
	Stay       string
	Booked     []store.ListItem
	Days       []itinDay // only days that have entries
}

// buildPlanSummary assembles the traveller-facing "the plan" section. Returns
// nil while there's nothing locked in or booked yet.
func buildPlanSummary(dateOpts []store.AxisOption, combos []store.Combo, itin, bookings *store.List, now time.Time) *planSummary {
	p := &planSummary{}

	locked := lockedOption(dateOpts)
	td := buildTravelData(locked, itin, nil, nil, now)
	if td.HasDates {
		p.DatesLabel = td.DatesLabel
		p.Countdown = td.Countdown
	}
	for _, day := range td.Days {
		if len(day.Entries) > 0 {
			p.Days = append(p.Days, day)
		}
	}

	if stay := chosenStay(combos); stay != nil {
		p.Stay = stay.Label
		if area := metaString(stay.Meta, "area"); area != "" {
			p.Stay += " · " + area
		}
	}

	if bookings != nil {
		for _, it := range bookings.Items {
			if st := metaString(it.Meta, "status"); st == "booked" || st == "paid" {
				p.Booked = append(p.Booked, it)
			}
		}
	}

	if p.DatesLabel == "" && p.Stay == "" && len(p.Booked) == 0 && len(p.Days) == 0 {
		return nil
	}
	return p
}
