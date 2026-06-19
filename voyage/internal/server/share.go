package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/baileybutler/voyage/internal/store"
)

// sortBudgetGroupsByVotes orders budgets by their vote tally.
func sortBudgetGroupsByVotes(g []budgetGroup) {
	sort.SliceStable(g, func(i, j int) bool { return g[i].Option.Votes > g[j].Option.Votes })
}

// sortOptionsByVotes orders axis options (e.g. date ranges) by their vote tally.
func sortOptionsByVotes(opts []store.AxisOption) {
	sort.SliceStable(opts, func(i, j int) bool { return opts[i].Votes > opts[j].Votes })
}

// sortComboItemsByVotes ranks accommodation within a budget by votes.
func sortComboItemsByVotes(items []store.ComboItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Votes > items[j].Votes })
}

// activityOrder returns activities in ranked order. When voterPoints is non-nil
// it reflects that traveller's personal ranking; otherwise it is the group's
// weighted ranking (summed points). Position is the tiebreak.
func activityOrder(items []store.ListItem, voterPoints map[int64]int, groupScores map[string]int) []store.ListItem {
	out := make([]store.ListItem, len(items))
	copy(out, items)
	score := func(it store.ListItem) int {
		if voterPoints != nil {
			return voterPoints[it.ID]
		}
		return groupScores[key("list_item", it.ID)]
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := score(out[i]), score(out[j])
		if si != sj {
			return si > sj
		}
		return out[i].Position < out[j].Position
	})
	return out
}

func voterCookieName(tripID int64) string { return "vv_" + strconv.FormatInt(tripID, 10) }

type sharePage struct {
	Title            string
	Trip             *store.Trip
	Voter            *store.Voter
	Budgets          []budgetGroup
	Dates            []store.AxisOption
	Activities       *store.List
	Token            string
	CanSuggestBudget bool // current traveller hasn't used their 1 budget suggestion
	CanSuggestDates  bool // ... their 1 dates suggestion
}

func (s *Server) loadVoter(r *http.Request, tripID int64) *store.Voter {
	c, err := r.Cookie(voterCookieName(tripID))
	if err != nil {
		return nil
	}
	v, err := s.store.VoterByToken(tripID, c.Value)
	if err != nil {
		return nil
	}
	return v
}

func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := s.store.GetTripByToken(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	voter := s.loadVoter(r, trip.ID)

	axes, _ := s.store.AxesForTrip(trip.ID)
	combos, _ := s.store.CombosForTrip(trip.ID)
	activities, _ := s.store.ActivitiesList(trip.ID)
	counts, _ := s.store.VoteCounts(trip.ID)

	voted := map[string]bool{}
	if voter != nil {
		voted, _ = s.store.VotesByVoter(voter.ID)
	}

	var budget, dates store.Axis
	for _, a := range axes {
		switch a.Kind {
		case "budget":
			budget = a
		case "date_range":
			dates = a
		}
	}

	// Everything ranks by the group's votes so favourites rise to the top.
	dateOpts := attachOptionVotes(dates.Options, counts, voted)
	nights, _ := referenceNights(dateOpts)
	budgets := s.budgetGroups(budget, combos, counts, voted, trip.PartySize, nights)
	sortBudgetGroupsByVotes(budgets)
	for i := range budgets {
		sortComboItemsByVotes(budgets[i].Hotels)
	}
	sortOptionsByVotes(dateOpts)

	// Activities: each traveller ranks them; show this traveller's personal order
	// (falling back to the group's weighted order before they've ranked).
	if activities != nil {
		var base map[int64]int
		if voter != nil {
			if vp, _ := s.store.VoterActivityPoints(voter.ID); len(vp) > 0 {
				base = vp
			}
		}
		activities.Items = activityOrder(activities.Items, base, counts)
	}

	// Traveller suggestion limits: 1 budget, 1 dates, 1 accommodation per budget,
	// unlimited activities.
	canBudget, canDates := false, false
	if voter != nil {
		if n, _ := s.store.CountAxisOptionsBy(budget.ID, voter.ID); n < 1 {
			canBudget = true
		}
		if n, _ := s.store.CountAxisOptionsBy(dates.ID, voter.ID); n < 1 {
			canDates = true
		}
		for i := range budgets {
			if budgets[i].ComboID == 0 {
				budgets[i].CanSuggestAccom = true
				continue
			}
			if n, _ := s.store.CountComboItemsBy(budgets[i].ComboID, voter.ID); n < 1 {
				budgets[i].CanSuggestAccom = true
			}
		}
	}

	s.render(w, "share", sharePage{
		Title:            trip.Title,
		Trip:             trip,
		Voter:            voter,
		Budgets:          budgets,
		Dates:            dateOpts,
		Activities:       activities,
		Token:            token,
		CanSuggestBudget: canBudget,
		CanSuggestDates:  canDates,
	})
}

func (s *Server) handleIdentify(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := s.store.GetTripByToken(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name != "" {
		if v, err := s.store.CreateVoter(trip.ID, name); err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     voterCookieName(trip.ID),
				Value:    v.Token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   60 * 60 * 24 * 365,
			})
		}
	}
	http.Redirect(w, r, "/t/"+token, http.StatusSeeOther)
}

func (s *Server) handleVote(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := s.store.GetTripByToken(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	voter := s.loadVoter(r, trip.ID)
	if voter == nil {
		http.Redirect(w, r, "/t/"+token, http.StatusSeeOther)
		return
	}
	targetType := strings.TrimSpace(r.FormValue("target_type"))
	if validVoteTarget(targetType) {
		_ = s.store.ToggleVote(trip.ID, voter.ID, targetType, formInt(r, "target_id"))
	}
	redirectBack(w, r, "/t/"+token)
}

// validVoteTarget guards what travellers can vote on. Activities (list_item) are
// ranked by the organiser, not voted on, so they are intentionally excluded.
func validVoteTarget(t string) bool {
	switch t {
	case "axis_option", "combo_item":
		return true
	}
	return false
}

// handleRank moves an activity up/down in the current traveller's personal
// ranking. Rankings are stored as Borda points and summed across travellers into
// the weighted final order. A traveller who hasn't ranked yet starts from the
// current group order and nudges from there.
func (s *Server) handleRank(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := s.store.GetTripByToken(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	voter := s.loadVoter(r, trip.ID)
	if voter == nil {
		http.Redirect(w, r, "/t/"+token, http.StatusSeeOther)
		return
	}
	itemID := formInt(r, "item")
	dir := strings.TrimSpace(r.FormValue("dir"))

	list, _ := s.store.ActivitiesList(trip.ID)
	if list == nil {
		redirectBack(w, r, "/t/"+token)
		return
	}
	vp, _ := s.store.VoterActivityPoints(voter.ID)
	var base map[int64]int
	if len(vp) > 0 {
		base = vp
	}
	counts, _ := s.store.VoteCounts(trip.ID)
	order := activityOrder(list.Items, base, counts)

	idx := -1
	for i := range order {
		if order[i].ID == itemID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		swap := idx
		if dir == "up" && idx > 0 {
			swap = idx - 1
		} else if dir == "down" && idx < len(order)-1 {
			swap = idx + 1
		}
		order[idx], order[swap] = order[swap], order[idx]
	}
	n := len(order)
	points := make(map[int64]int, n)
	for i, it := range order {
		points[it.ID] = n - i // top gets n points
	}
	_ = s.store.SetVoterActivityPoints(trip.ID, voter.ID, points)
	redirectBack(w, r, "/t/"+token)
}

// ---- traveller suggestions ----

// shareTripAndVoter resolves the trip + identified voter for a /t/{token} POST,
// writing the redirect/response itself and returning ok=false when it can't.
func (s *Server) shareTripAndVoter(w http.ResponseWriter, r *http.Request) (string, *store.Trip, *store.Voter, bool) {
	token := r.PathValue("token")
	trip, err := s.store.GetTripByToken(token)
	if err != nil {
		http.NotFound(w, r)
		return token, nil, nil, false
	}
	voter := s.loadVoter(r, trip.ID)
	if voter == nil {
		http.Redirect(w, r, "/t/"+token, http.StatusSeeOther)
		return token, trip, nil, false
	}
	return token, trip, voter, true
}

func (s *Server) shareAxes(tripID int64) (budget, dates store.Axis) {
	axes, _ := s.store.AxesForTrip(tripID)
	for _, a := range axes {
		switch a.Kind {
		case "budget":
			budget = a
		case "date_range":
			dates = a
		}
	}
	return
}

// handleSuggestBudget lets a traveller add one budget option of their own.
func (s *Server) handleSuggestBudget(w http.ResponseWriter, r *http.Request) {
	token, trip, voter, ok := s.shareTripAndVoter(w, r)
	if !ok {
		return
	}
	budget, _ := s.shareAxes(trip.ID)
	if n, _ := s.store.CountAxisOptionsBy(budget.ID, voter.ID); n < 1 {
		if label, meta := optionFromForm("budget", r); label != "" {
			_, _ = s.store.AddAxisOption(budget.ID, label, meta, &voter.ID)
		}
	}
	redirectBack(w, r, "/t/"+token)
}

// handleSuggestDates lets a traveller add one date range of their own.
func (s *Server) handleSuggestDates(w http.ResponseWriter, r *http.Request) {
	token, trip, voter, ok := s.shareTripAndVoter(w, r)
	if !ok {
		return
	}
	_, dates := s.shareAxes(trip.ID)
	if n, _ := s.store.CountAxisOptionsBy(dates.ID, voter.ID); n < 1 {
		if label, meta := optionFromForm("date_range", r); label != "" {
			_, _ = s.store.AddAxisOption(dates.ID, label, meta, &voter.ID)
		}
	}
	redirectBack(w, r, "/t/"+token)
}

// handleSuggestAccommodation lets a traveller add one stay under a given budget.
func (s *Server) handleSuggestAccommodation(w http.ResponseWriter, r *http.Request) {
	token, trip, voter, ok := s.shareTripAndVoter(w, r)
	if !ok {
		return
	}
	optID := formInt(r, "budget_option_id")
	axisID, err := s.store.AxisOptionAxisID(optID)
	if err != nil {
		redirectBack(w, r, "/t/"+token)
		return
	}
	comboID, err := s.store.EnsureCombo(trip.ID, map[int64]int64{axisID: optID})
	if err != nil {
		redirectBack(w, r, "/t/"+token)
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if n, _ := s.store.CountComboItemsBy(comboID, voter.ID); n < 1 && label != "" {
		_, _ = s.store.AddComboItem(comboID, "hotel", label,
			strings.TrimSpace(r.FormValue("link")), strings.TrimSpace(r.FormValue("notes")), hotelMetaFromForm(r), &voter.ID)
	}
	redirectBack(w, r, "/t/"+token)
}

// handleSuggestActivity lets a traveller add activities (unlimited).
func (s *Server) handleSuggestActivity(w http.ResponseWriter, r *http.Request) {
	token, trip, voter, ok := s.shareTripAndVoter(w, r)
	if !ok {
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if label != "" {
		if list, err := s.store.ActivitiesList(trip.ID); err == nil {
			_, _ = s.store.AddListItem(list.ID, label, strings.TrimSpace(r.FormValue("notes")), strings.TrimSpace(r.FormValue("link")), &voter.ID)
		}
	}
	redirectBack(w, r, "/t/"+token)
}
