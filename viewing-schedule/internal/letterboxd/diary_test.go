package letterboxd

import "testing"

const sampleDiaryHTML = `
<table>
<tbody>
<tr class="diary-entry-row viewing-poster-container" data-viewing-id="1287338936" data-owner="baely">
  <td class="col-monthdate -align-center"><div class="monthdate">
    <a class="month" href="/baely/diary/films/for/2026/04/">Apr</a>
    <a class="year" href="/baely/diary/films/for/2026/">2026</a>
  </div></td>
  <td class="col-daydate -align-center"><a class="daydate" href="/baely/diary/films/for/2026/04/20/">20</a></td>
  <td class="col-production">
    <div class="react-component figure" data-item-name="Schindler&#039;s List (1993)" data-item-slug="schindlers-list" data-item-link="/film/schindlers-list/"></div>
  </td>
  <td class="col-releaseyear"><span>1993</span></td>
  <td class="col-rating"><div class="rating-green"><span class="rating rated-9">★★★★½</span></div></td>
  <td class="col-like"><span class="like-link-target react-component"></span><span class="has-icon icon-like icon-status-on"></span></td>
  <td class="col-rewatch"><span class="has-icon icon-rewatch icon-status-on"></span></td>
  <td class="col-review"></td>
</tr>

<tr class="diary-entry-row" data-viewing-id="42" data-owner="baely">
  <td class="col-monthdate"><div class="monthdate">
    <a class="month" href="/baely/diary/films/for/2025/12/">Dec</a>
    <a class="year" href="/baely/diary/films/for/2025/">2025</a>
  </div></td>
  <td class="col-daydate"><a class="daydate" href="/baely/diary/films/for/2025/12/01/">1</a></td>
  <td class="col-production">
    <div class="react-component" data-item-name="A Film With No Year" data-item-slug="a-film-with-no-year" data-item-link="/film/a-film-with-no-year/"></div>
  </td>
  <td class="col-releaseyear"><span>2024</span></td>
  <td class="col-rating"></td>
  <td class="col-like"><span class="has-icon icon-like icon-status-off"></span></td>
  <td class="col-rewatch"><span class="has-icon icon-rewatch icon-status-off"></span></td>
  <td class="col-review"></td>
</tr>
</tbody>
</table>
`

func TestParseDiary(t *testing.T) {
	got := parseDiary(sampleDiaryHTML)
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %#v", len(got), got)
	}

	a := got[0]
	if a.ViewingID != 1287338936 {
		t.Errorf("viewing id = %d", a.ViewingID)
	}
	if a.WatchedDate != "2026-04-20" {
		t.Errorf("watched date = %q", a.WatchedDate)
	}
	if a.Slug != "schindlers-list" {
		t.Errorf("slug = %q", a.Slug)
	}
	if a.Title != "Schindler's List" {
		t.Errorf("title = %q (apostrophe should be decoded)", a.Title)
	}
	if a.Year != "1993" {
		t.Errorf("year = %q", a.Year)
	}
	if a.Rating != 4.5 {
		t.Errorf("rating = %v, want 4.5", a.Rating)
	}
	if !a.Liked {
		t.Errorf("liked = false, want true")
	}
	if !a.Rewatch {
		t.Errorf("rewatch = false, want true")
	}

	b := got[1]
	if b.ViewingID != 42 || b.WatchedDate != "2025-12-01" {
		t.Errorf("entry 2 fields wrong: %+v", b)
	}
	if b.Title != "A Film With No Year" || b.Year != "" {
		t.Errorf("entry 2 title/year: %+v", b)
	}
	if b.Rating != 0 {
		t.Errorf("entry 2 rating = %v, want 0", b.Rating)
	}
	if b.Liked || b.Rewatch {
		t.Errorf("entry 2 liked/rewatch = %v/%v, want false/false", b.Liked, b.Rewatch)
	}
}
