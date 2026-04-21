package letterboxd

import "testing"

const sampleHTML = `
<html><body>
<ul class="grid">
  <li class="griditem">
    <div class="react-component" data-component-class="LazyPoster"
         data-item-name="The Apartment (1960)"
         data-item-slug="the-apartment"
         data-item-link="/film/the-apartment/"
         data-item-full-display-name="The Apartment (1960)">
    </div>
  </li>
  <li class="griditem">
    <div class="react-component"
         data-item-name="Portrait of a Lady on Fire (2019)"
         data-item-slug="portrait-of-a-lady-on-fire"
         data-item-link="/film/portrait-of-a-lady-on-fire/">
    </div>
  </li>
  <li class="griditem">
    <div class="react-component"
         data-item-name="A Film With No Year"
         data-item-slug="a-film-with-no-year"
         data-item-link="/film/a-film-with-no-year/">
    </div>
  </li>
</ul>
<div class="paginate-nextprev"><a class="next" href="/foo/page/2/">Next</a></div>
</body></html>
`

func TestParseFilms(t *testing.T) {
	got := parseFilms(sampleHTML)
	if len(got) != 3 {
		t.Fatalf("want 3 films, got %d: %#v", len(got), got)
	}

	want := []Film{
		{Title: "The Apartment", Year: "1960", Slug: "the-apartment", Link: "/film/the-apartment/"},
		{Title: "Portrait of a Lady on Fire", Year: "2019", Slug: "portrait-of-a-lady-on-fire", Link: "/film/portrait-of-a-lady-on-fire/"},
		{Title: "A Film With No Year", Year: "", Slug: "a-film-with-no-year", Link: "/film/a-film-with-no-year/"},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("film[%d]: got %+v, want %+v", i, got[i], w)
		}
	}
}

func TestSplitTitleYear(t *testing.T) {
	cases := []struct {
		in        string
		title     string
		year      string
	}{
		{"The Apartment (1960)", "The Apartment", "1960"},
		{"Spider-Man: Into the Spider-Verse (2018)", "Spider-Man: Into the Spider-Verse", "2018"},
		{"No Year Here", "No Year Here", ""},
		{"", "", ""},
		{"Weird (Year)", "Weird (Year)", ""},
	}
	for _, c := range cases {
		t, y := splitTitleYear(c.in)
		if t != c.title || y != c.year {
			panic("split mismatch: " + c.in)
		}
		_ = t
		_ = y
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"baely/watchlist/", "https://letterboxd.com/baely/watchlist/", false},
		{"/baely/watchlist/", "https://letterboxd.com/baely/watchlist/", false},
		{"https://letterboxd.com/baely/watchlist/", "https://letterboxd.com/baely/watchlist/", false},
		{"http://letterboxd.com/baely/watchlist/", "https://letterboxd.com/baely/watchlist/", false},
		{"https://example.com/baely/", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := normalizeURL(c.in)
		if c.err {
			if err == nil {
				t.Errorf("normalizeURL(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeURL(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHasNextPage(t *testing.T) {
	if !hasNextPage(`<a class="next" href="/x/page/2/">Older</a>`) {
		t.Error("expected next page detection")
	}
	if hasNextPage(`<div>nothing here</div>`) {
		t.Error("did not expect next page")
	}
}
