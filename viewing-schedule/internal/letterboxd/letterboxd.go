// Package letterboxd implements an HTML scraper for Letterboxd.
//
// Letterboxd's public API was retired, so we have to screen-scrape. To make our
// requests look like a normal browser we send a desktop Chrome User-Agent and
// the same Accept headers a browser would send.
//
// We support importing films from any of these page types for a given user:
//   - https://letterboxd.com/<user>/watchlist/
//   - https://letterboxd.com/<user>/films/
//   - https://letterboxd.com/<user>/films/diary/
//   - https://letterboxd.com/<user>/list/<slug>/
//
// On every page Letterboxd renders each film with attributes like:
//
//	data-item-name="The Apartment (1960)"
//	data-item-slug="the-apartment"
//	data-item-link="/film/the-apartment/"
//
// We follow `next` pagination (`/page/N/`) until exhausted or a safety cap is
// reached.
package letterboxd

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// BrowserUA is a recent desktop Chrome on macOS. Letterboxd happily serves
	// HTML to this; using the default Go UA can trigger 403/Cloudflare.
	BrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	// MaxPages is a hard cap on pagination per import to keep runs bounded.
	MaxPages = 50
)

// Film is a single scraped film.
type Film struct {
	Title string // e.g. "The Apartment"
	Year  string // e.g. "1960"; may be empty
	Slug  string // letterboxd slug, e.g. "the-apartment"
	Link  string // e.g. "/film/the-apartment/"
}

// FullTitle returns "Title (Year)" or just Title when year is unknown.
func (f Film) FullTitle() string {
	if f.Year == "" {
		return f.Title
	}
	return fmt.Sprintf("%s (%s)", f.Title, f.Year)
}

// Client scrapes letterboxd.com pages.
type Client struct {
	HTTP *http.Client
	UA   string
}

// New constructs a default Client with a 20s HTTP timeout and a browser UA.
func New() *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 20 * time.Second},
		UA:   BrowserUA,
	}
}

// FetchURL fetches and parses any single Letterboxd page that lists films,
// following pagination automatically. Accepts either a full https URL or a
// path like "/baely/watchlist/".
func (c *Client) FetchURL(ctx context.Context, raw string) ([]Film, error) {
	base, err := normalizeURL(raw)
	if err != nil {
		return nil, err
	}
	return c.fetchPaginated(ctx, base)
}

// FetchWatchlist fetches a user's public watchlist.
func (c *Client) FetchWatchlist(ctx context.Context, user string) ([]Film, error) {
	if user == "" {
		return nil, errors.New("letterboxd: user required")
	}
	return c.fetchPaginated(ctx, "https://letterboxd.com/"+url.PathEscape(user)+"/watchlist/")
}

// FetchList fetches a user-defined list.
func (c *Client) FetchList(ctx context.Context, user, slug string) ([]Film, error) {
	if user == "" || slug == "" {
		return nil, errors.New("letterboxd: user and list slug required")
	}
	return c.fetchPaginated(ctx, "https://letterboxd.com/"+url.PathEscape(user)+"/list/"+url.PathEscape(slug)+"/")
}

func (c *Client) fetchPaginated(ctx context.Context, base string) ([]Film, error) {
	base = strings.TrimRight(base, "/") + "/"
	seen := map[string]struct{}{}
	var out []Film
	for page := 1; page <= MaxPages; page++ {
		u := base
		if page > 1 {
			u = base + "page/" + fmt.Sprint(page) + "/"
		}
		body, err := c.fetch(ctx, u)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			// Subsequent page failures end pagination gracefully.
			break
		}
		films := parseFilms(body)
		if len(films) == 0 {
			break
		}
		added := 0
		for _, f := range films {
			key := f.Slug + "|" + f.Year
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, f)
			added++
		}
		if added == 0 {
			break // page repeated previous content
		}
		if !hasNextPage(body) {
			break
		}
	}
	return out, nil
}

func (c *Client) fetch(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.UA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "identity") // avoid gzip handling
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("letterboxd: page not found (%s)", u)
	}
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("letterboxd: status %d for %s", res.StatusCode, u)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20)) // 8MB cap per page
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// reItem matches a Letterboxd react-component poster element. The order of
// data-* attributes on the rendered HTML is stable enough to use a regex; if it
// ever changes we fall back to slug-only matching via reSlug.
var (
	reName = regexp.MustCompile(`data-item-name="([^"]+)"`)
	reSlug = regexp.MustCompile(`data-item-slug="([^"]+)"`)
	reLink = regexp.MustCompile(`data-item-link="(/film/[^"]+)"`)
	reYear = regexp.MustCompile(`\((\d{4})\)\s*$`)
)

// parseFilms extracts films from a Letterboxd HTML page.
//
// Each film card on Letterboxd renders something like:
//
//	<div class="react-component" data-component-class="LazyPoster"
//	     data-item-name="The Apartment (1960)"
//	     data-item-slug="the-apartment"
//	     data-item-link="/film/the-apartment/" ...>
//
// We anchor on `data-item-link="/film/..."` because every film card has one.
// For each match, we look in a small window before AND after the link for the
// matching `data-item-name` attribute on the same card. The window is bounded
// to (a) the previous link occurrence and (b) the next link occurrence so we
// never pick up a neighbouring card's name.
func parseFilms(body string) []Film {
	const linkAttr = `data-item-link="/film/`

	// Collect the byte positions of every film link occurrence.
	var positions []int
	from := 0
	for {
		i := strings.Index(body[from:], linkAttr)
		if i < 0 {
			break
		}
		positions = append(positions, from+i)
		from += i + len(linkAttr)
	}
	if len(positions) == 0 {
		return nil
	}

	out := make([]Film, 0, len(positions))
	seen := map[string]struct{}{} // dedupe within a single page

	for idx, pos := range positions {
		linkStart := pos + len(linkAttr)
		end := strings.IndexByte(body[linkStart:], '"')
		if end <= 0 {
			continue
		}
		linkPath := "/film/" + body[linkStart:linkStart+end]
		slug := strings.Trim(body[linkStart:linkStart+end], "/")

		// Window: from end of previous link's slug to start of next link.
		winStart := 0
		if idx > 0 {
			prev := positions[idx-1]
			// Skip over the previous link's value to avoid contamination.
			if e := strings.IndexByte(body[prev+len(linkAttr):], '"'); e >= 0 {
				winStart = prev + len(linkAttr) + e + 1
			} else {
				winStart = prev + len(linkAttr)
			}
		}
		winEnd := len(body)
		if idx+1 < len(positions) {
			winEnd = positions[idx+1]
		}
		// Bound the window to a reasonable card size (~8KB) on either side.
		if pos-winStart > 8192 {
			winStart = pos - 8192
		}
		if winEnd-pos > 8192 {
			winEnd = pos + 8192
		}
		window := body[winStart:winEnd]

		var name string
		if m := reName.FindStringSubmatch(window); len(m) == 2 {
			// Letterboxd HTML-encodes the attribute value (e.g. &#039;s).
			name = html.UnescapeString(m[1])
		}
		title, year := splitTitleYear(name)
		if title == "" {
			title = slugToTitle(slug)
		}

		key := slug + "|" + year
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, Film{
			Title: title,
			Year:  year,
			Slug:  slug,
			Link:  linkPath,
		})
	}
	_ = reSlug
	_ = reLink
	return out
}

// splitTitleYear pulls the year out of "Title (1999)".
func splitTitleYear(name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	if m := reYear.FindStringSubmatchIndex(name); len(m) == 4 {
		title := strings.TrimSpace(name[:m[0]])
		year := name[m[2]:m[3]]
		return title, year
	}
	return name, ""
}

// slugToTitle is a last-ditch fallback that turns "the-apartment" into
// "The Apartment".
func slugToTitle(slug string) string {
	slug = strings.Trim(slug, "/")
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// hasNextPage looks for Letterboxd's pagination "next" link.
func hasNextPage(html string) bool {
	// Letterboxd renders the next link as <a class="next" href="...">.
	if strings.Contains(html, `class="next"`) {
		return true
	}
	// Some templates use a paginate-nextprev block.
	if strings.Contains(html, `paginate-nextprev`) && strings.Contains(html, `class="next`) {
		return true
	}
	return false
}

// normalizeURL accepts a path or full URL and returns a canonical https URL.
func normalizeURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("letterboxd: empty url")
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", err
		}
		if !strings.HasSuffix(u.Host, "letterboxd.com") {
			return "", fmt.Errorf("letterboxd: refusing to scrape host %q", u.Host)
		}
		u.Scheme = "https"
		return u.String(), nil
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return "https://letterboxd.com" + s, nil
}
