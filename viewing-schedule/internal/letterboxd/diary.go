package letterboxd

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// DiaryEntry is one viewing logged in a user's Letterboxd diary.
type DiaryEntry struct {
	ViewingID   int64   // Letterboxd's stable ID for this viewing
	WatchedDate string  // YYYY-MM-DD
	Slug        string  // film slug, e.g. "the-apartment"
	Title       string  // film title, HTML-decoded
	Year        string  // release year
	Rating      float64 // 0.5..5.0 stars; 0 means unrated
	Liked       bool
	Rewatch     bool
	HasReview   bool
}

// FullTitle returns "Title (Year)".
func (d DiaryEntry) FullTitle() string {
	if d.Year == "" {
		return d.Title
	}
	return fmt.Sprintf("%s (%s)", d.Title, d.Year)
}

// FetchDiary scrapes a user's diary. The optional stop function is called for
// every parsed entry; if it returns true, scraping halts (used for incremental
// sync to bail out once we hit known viewings).
func (c *Client) FetchDiary(ctx context.Context, user string, stop func(DiaryEntry) bool) ([]DiaryEntry, error) {
	if user == "" {
		return nil, errors.New("letterboxd: user required")
	}
	base := "https://letterboxd.com/" + url.PathEscape(user) + "/films/diary/"
	var out []DiaryEntry
	seen := map[int64]struct{}{}
	for page := 1; page <= MaxPages; page++ {
		u := base
		if page > 1 {
			u = base + "page/" + strconv.Itoa(page) + "/"
		}
		body, err := c.fetch(ctx, u)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		entries := parseDiary(body)
		if len(entries) == 0 {
			break
		}
		newOnPage := 0
		for _, e := range entries {
			if _, ok := seen[e.ViewingID]; ok {
				continue
			}
			seen[e.ViewingID] = struct{}{}
			out = append(out, e)
			newOnPage++
			if stop != nil && stop(e) {
				return out, nil
			}
		}
		if newOnPage == 0 || !hasNextPage(body) {
			break
		}
	}
	return out, nil
}

var (
	// reDiaryRow captures the entire <tr ...>...</tr> (including the opening
	// tag attributes), since data-viewing-id lives on the <tr> itself.
	reDiaryRow      = regexp.MustCompile(`(?s)(<tr[^>]*class="diary-entry-row[^"]*"[^>]*>.*?</tr>)`)
	reViewingID     = regexp.MustCompile(`data-viewing-id="(\d+)"`)
	reDayURL        = regexp.MustCompile(`/(?:diary/films|films/diary)/for/(\d{4})/(\d{2})/(\d{2})/`)
	reRatingClass   = regexp.MustCompile(`class="rating[^"]*\brated-(\d+)\b`)
	reLikedOn       = regexp.MustCompile(`icon-like[^"]*\bicon-status-on\b|\bicon-liked\b`)
	reRewatchOn     = regexp.MustCompile(`icon-rewatch[^"]*\bicon-status-on\b`)
	reReviewLink    = regexp.MustCompile(`href="/[^/"]+/film/[^"]+/[^"]*\d+/(?:#review|reviews/)`)
	reReviewIcon    = regexp.MustCompile(`\bicon-review\b`)
	reItemNameDiary = regexp.MustCompile(`data-item-name="([^"]+)"`)
	reItemSlugDiary = regexp.MustCompile(`data-item-slug="([^"]+)"`)
)

// parseDiary parses the rows from a diary page.
func parseDiary(body string) []DiaryEntry {
	matches := reDiaryRow.FindAllStringSubmatch(body, -1)
	out := make([]DiaryEntry, 0, len(matches))
	for _, m := range matches {
		row := m[1]
		entry, ok := parseDiaryRow(row)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func parseDiaryRow(row string) (DiaryEntry, bool) {
	var e DiaryEntry
	if m := reViewingID.FindStringSubmatch(row); len(m) == 2 {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return e, false
		}
		e.ViewingID = id
	} else {
		return e, false
	}

	if m := reDayURL.FindStringSubmatch(row); len(m) == 4 {
		e.WatchedDate = m[1] + "-" + m[2] + "-" + m[3]
	}

	if m := reItemSlugDiary.FindStringSubmatch(row); len(m) == 2 {
		e.Slug = m[1]
	}
	if m := reItemNameDiary.FindStringSubmatch(row); len(m) == 2 {
		name := html.UnescapeString(m[1])
		e.Title, e.Year = splitTitleYear(name)
		if e.Title == "" {
			e.Title = slugToTitle(e.Slug)
		}
	} else if e.Slug != "" {
		e.Title = slugToTitle(e.Slug)
	}

	if m := reRatingClass.FindStringSubmatch(row); len(m) == 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil && n >= 1 && n <= 10 {
			e.Rating = float64(n) / 2.0
		}
	}
	e.Liked = reLikedOn.MatchString(row)
	e.Rewatch = reRewatchOn.MatchString(row)
	e.HasReview = reReviewLink.MatchString(row) || reReviewIcon.MatchString(row) ||
		strings.Contains(row, "has-review") // fallback marker class
	return e, e.ViewingID != 0
}
