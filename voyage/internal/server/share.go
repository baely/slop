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
	Title      string
	Trip       *store.Trip
	Voter      *store.Voter
	Budgets    []budgetGroup
	Dates      []store.AxisOption
	Activities *store.List
	Comments   map[string][]store.Comment
	Token      string
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
	comments, _ := s.store.CommentsFor(trip.ID)

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

	s.render(w, "share", sharePage{
		Title:      trip.Title,
		Trip:       trip,
		Voter:      voter,
		Budgets:    budgets,
		Dates:      dateOpts,
		Activities: activities,
		Comments:   comments,
		Token:      token,
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

// validCommentTarget guards what travellers can comment on (everything, incl.
// activities).
func validCommentTarget(t string) bool {
	return t == "list_item" || validVoteTarget(t)
}

func (s *Server) handleShareComment(w http.ResponseWriter, r *http.Request) {
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
	body := strings.TrimSpace(r.FormValue("body"))
	targetType := strings.TrimSpace(r.FormValue("target_type"))
	if body != "" && validCommentTarget(targetType) {
		_ = s.store.AddComment(trip.ID, &voter.ID, targetType, formInt(r, "target_id"), body)
	}
	redirectBack(w, r, "/t/"+token)
}

func (s *Server) handleShareDeleteComment(w http.ResponseWriter, r *http.Request) {
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
	_ = s.store.DeleteComment(parseID(r, "id"), &voter.ID) // travellers delete only their own
	redirectBack(w, r, "/t/"+token)
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
