package store

import (
	"database/sql"
	"time"
)

// Image is a cached hero-photo lookup for a place query. An empty URL is a
// cached miss, kept so failing queries aren't retried on every render.
type Image struct {
	Query     string
	URL       string
	Credit    string
	CreditURL string
	FetchedAt string
}

// GetImage returns the cached image for a query, or ErrNotFound.
func (s *Store) GetImage(query string) (*Image, error) {
	var img Image
	err := s.db.QueryRow(
		`SELECT query, url, credit, credit_url, fetched_at FROM images WHERE query = ?`, query).
		Scan(&img.Query, &img.URL, &img.Credit, &img.CreditURL, &img.FetchedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &img, nil
}

// PutImage upserts a query's image lookup result.
func (s *Store) PutImage(img Image) error {
	_, err := s.db.Exec(
		`INSERT INTO images (query, url, credit, credit_url, fetched_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(query) DO UPDATE SET url = excluded.url, credit = excluded.credit,
		 credit_url = excluded.credit_url, fetched_at = excluded.fetched_at`,
		img.Query, img.URL, img.Credit, img.CreditURL, now())
	return err
}

// ImageFetchedAt parses an image row's fetch time (zero time when unparseable).
func (i *Image) FetchedTime() time.Time {
	t, _ := time.Parse(time.RFC3339, i.FetchedAt)
	return t
}
