package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database holding the viewing schedule.
type Store struct {
	db *sql.DB
}

// Movie is a single film attached to a schedule entry.
type Movie struct {
	ID          int64
	EntryID     int64
	Position    int    // 0 or 1 within an entry
	Title       string // raw user-entered title (e.g. "The Apartment (1960)")
	Year        string
	TMDBID      int64
	TMDBTitle   string
	Overview    string
	Director    string
	Runtime     int
	Genres      string // comma-separated
	Rating      float64
	Poster      string
	PosterLg    string
	Backdrop    string
	ReleaseDate string
	Tagline     string
	UpdatedAt   time.Time
}

// Entry is one date on the schedule.
type Entry struct {
	ID      int64
	Date    string // YYYY-MM-DD
	Day     string // weekday
	Reason  string
	Movies  []Movie
}

// Open opens the SQLite database at the given path.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("db path is empty")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite + WAL is happiest with serialized writes
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Migrate creates required tables if they do not exist.
func (s *Store) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL UNIQUE,
			day TEXT,
			reason TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS movies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			position INTEGER NOT NULL DEFAULT 0,
			title TEXT NOT NULL,
			year TEXT,
			tmdb_id INTEGER,
			tmdb_title TEXT,
			overview TEXT,
			director TEXT,
			runtime INTEGER,
			genres TEXT,
			rating REAL,
			poster TEXT,
			poster_lg TEXT,
			backdrop TEXT,
			release_date TEXT,
			tagline TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_movies_entry ON movies(entry_id);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS viewings (
			viewing_id   INTEGER PRIMARY KEY,
			user         TEXT    NOT NULL,
			watched_date TEXT    NOT NULL,
			film_slug    TEXT    NOT NULL,
			film_title   TEXT    NOT NULL,
			film_year    TEXT,
			rating       REAL,
			liked        INTEGER NOT NULL DEFAULT 0,
			rewatch      INTEGER NOT NULL DEFAULT 0,
			has_review   INTEGER NOT NULL DEFAULT 0,
			tmdb_id      INTEGER,
			tmdb_title   TEXT,
			director     TEXT,
			runtime      INTEGER,
			genres       TEXT,
			tmdb_rating  REAL,
			poster       TEXT,
			poster_lg    TEXT,
			backdrop     TEXT,
			overview     TEXT,
			tagline      TEXT,
			release_date TEXT,
			synced_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS idx_viewings_user_date ON viewings(user, watched_date);`,
		`CREATE INDEX IF NOT EXISTS idx_viewings_slug ON viewings(film_slug);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// ListEntries returns all entries (with movies) ordered by date.
func (s *Store) ListEntries() ([]Entry, error) {
	rows, err := s.db.Query(`SELECT id, date, COALESCE(day,''), COALESCE(reason,'') FROM entries ORDER BY date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	idx := map[int64]int{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Date, &e.Day, &e.Reason); err != nil {
			return nil, err
		}
		idx[e.ID] = len(entries)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mrows, err := s.db.Query(`SELECT id, entry_id, position, title, COALESCE(year,''), COALESCE(tmdb_id,0),
		COALESCE(tmdb_title,''), COALESCE(overview,''), COALESCE(director,''), COALESCE(runtime,0),
		COALESCE(genres,''), COALESCE(rating,0), COALESCE(poster,''), COALESCE(poster_lg,''),
		COALESCE(backdrop,''), COALESCE(release_date,''), COALESCE(tagline,'')
		FROM movies ORDER BY entry_id, position`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var m Movie
		if err := mrows.Scan(&m.ID, &m.EntryID, &m.Position, &m.Title, &m.Year, &m.TMDBID,
			&m.TMDBTitle, &m.Overview, &m.Director, &m.Runtime, &m.Genres, &m.Rating,
			&m.Poster, &m.PosterLg, &m.Backdrop, &m.ReleaseDate, &m.Tagline); err != nil {
			return nil, err
		}
		if i, ok := idx[m.EntryID]; ok {
			entries[i].Movies = append(entries[i].Movies, m)
		}
	}
	return entries, mrows.Err()
}

// UpsertEntry creates or updates an entry by date and returns the entry id.
func (s *Store) UpsertEntry(date, day, reason string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO entries(date, day, reason) VALUES(?,?,?)
		ON CONFLICT(date) DO UPDATE SET day=excluded.day, reason=excluded.reason`,
		date, day, reason)
	if err != nil {
		return 0, err
	}
	if id, err := res.LastInsertId(); err == nil && id > 0 {
		// LastInsertId may be the actual row id even on upsert; verify by lookup.
		var rowID int64
		if err := s.db.QueryRow(`SELECT id FROM entries WHERE date=?`, date).Scan(&rowID); err == nil {
			return rowID, nil
		}
		return id, nil
	}
	var rowID int64
	if err := s.db.QueryRow(`SELECT id FROM entries WHERE date=?`, date).Scan(&rowID); err != nil {
		return 0, err
	}
	return rowID, nil
}

// DeleteEntry removes an entry and its movies.
func (s *Store) DeleteEntry(id int64) error {
	_, err := s.db.Exec(`DELETE FROM entries WHERE id=?`, id)
	return err
}

// ReplaceMovies deletes and re-inserts the movies for an entry.
func (s *Store) ReplaceMovies(entryID int64, movies []Movie) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM movies WHERE entry_id=?`, entryID); err != nil {
		return err
	}
	for i, m := range movies {
		_, err := tx.Exec(`INSERT INTO movies(entry_id, position, title, year, tmdb_id, tmdb_title,
			overview, director, runtime, genres, rating, poster, poster_lg, backdrop, release_date, tagline, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
			entryID, i, m.Title, m.Year, m.TMDBID, m.TMDBTitle, m.Overview, m.Director,
			m.Runtime, m.Genres, m.Rating, m.Poster, m.PosterLg, m.Backdrop, m.ReleaseDate, m.Tagline)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetSetting returns a setting value or empty string.
func (s *Store) GetSetting(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// Viewing is one Letterboxd diary entry, optionally enriched with TMDB
// metadata cached at sync time.
type Viewing struct {
	ViewingID   int64
	User        string
	WatchedDate string
	Slug        string
	Title       string
	Year        string
	Rating      float64 // 0..5; 0 if unrated
	Liked       bool
	Rewatch     bool
	HasReview   bool

	TMDBID      int64
	TMDBTitle   string
	Director    string
	Runtime     int
	Genres      string // comma-separated
	TMDBRating  float64
	Poster      string
	PosterLg    string
	Backdrop    string
	Overview    string
	Tagline     string
	ReleaseDate string

	SyncedAt time.Time
}

// HasTMDB reports whether this viewing has cached TMDB metadata.
func (v Viewing) HasTMDB() bool { return v.TMDBID != 0 }

// UpsertViewing inserts or updates a viewing keyed by viewing_id. If the
// caller has not provided TMDB fields, the existing cached values are
// preserved (so re-running a diary sync never wipes enrichment).
func (s *Store) UpsertViewing(v Viewing) error {
	_, err := s.db.Exec(`
		INSERT INTO viewings(
			viewing_id, user, watched_date, film_slug, film_title, film_year,
			rating, liked, rewatch, has_review,
			tmdb_id, tmdb_title, director, runtime, genres, tmdb_rating,
			poster, poster_lg, backdrop, overview, tagline, release_date, synced_at
		) VALUES (?,?,?,?,?,?, ?,?,?,?, ?,?,?,?,?,?, ?,?,?,?,?,?, CURRENT_TIMESTAMP)
		ON CONFLICT(viewing_id) DO UPDATE SET
			user=excluded.user,
			watched_date=excluded.watched_date,
			film_slug=excluded.film_slug,
			film_title=excluded.film_title,
			film_year=excluded.film_year,
			rating=excluded.rating,
			liked=excluded.liked,
			rewatch=excluded.rewatch,
			has_review=excluded.has_review,
			tmdb_id=COALESCE(NULLIF(excluded.tmdb_id,0), viewings.tmdb_id),
			tmdb_title=COALESCE(NULLIF(excluded.tmdb_title,''), viewings.tmdb_title),
			director=COALESCE(NULLIF(excluded.director,''), viewings.director),
			runtime=COALESCE(NULLIF(excluded.runtime,0), viewings.runtime),
			genres=COALESCE(NULLIF(excluded.genres,''), viewings.genres),
			tmdb_rating=COALESCE(NULLIF(excluded.tmdb_rating,0), viewings.tmdb_rating),
			poster=COALESCE(NULLIF(excluded.poster,''), viewings.poster),
			poster_lg=COALESCE(NULLIF(excluded.poster_lg,''), viewings.poster_lg),
			backdrop=COALESCE(NULLIF(excluded.backdrop,''), viewings.backdrop),
			overview=COALESCE(NULLIF(excluded.overview,''), viewings.overview),
			tagline=COALESCE(NULLIF(excluded.tagline,''), viewings.tagline),
			release_date=COALESCE(NULLIF(excluded.release_date,''), viewings.release_date),
			synced_at=CURRENT_TIMESTAMP
		`,
		v.ViewingID, v.User, v.WatchedDate, v.Slug, v.Title, v.Year,
		nullableFloat(v.Rating), boolToInt(v.Liked), boolToInt(v.Rewatch), boolToInt(v.HasReview),
		v.TMDBID, v.TMDBTitle, v.Director, v.Runtime, v.Genres, v.TMDBRating,
		v.Poster, v.PosterLg, v.Backdrop, v.Overview, v.Tagline, v.ReleaseDate,
	)
	return err
}

// HasViewing reports whether a viewing_id is already stored.
func (s *Store) HasViewing(id int64) bool {
	var x int
	_ = s.db.QueryRow(`SELECT 1 FROM viewings WHERE viewing_id=?`, id).Scan(&x)
	return x == 1
}

// CountViewings returns the total number of stored viewings for a user
// (or all users when user is empty).
func (s *Store) CountViewings(user string) (int, error) {
	q := `SELECT COUNT(*) FROM viewings`
	args := []any{}
	if user != "" {
		q += ` WHERE user=?`
		args = append(args, user)
	}
	var n int
	err := s.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// ListViewings returns viewings filtered by user and optional date range
// (inclusive). Empty filters mean "all".
func (s *Store) ListViewings(user, fromDate, toDate string) ([]Viewing, error) {
	q := `SELECT viewing_id, user, watched_date, film_slug, film_title,
		COALESCE(film_year,''), COALESCE(rating,0), liked, rewatch, has_review,
		COALESCE(tmdb_id,0), COALESCE(tmdb_title,''), COALESCE(director,''),
		COALESCE(runtime,0), COALESCE(genres,''), COALESCE(tmdb_rating,0),
		COALESCE(poster,''), COALESCE(poster_lg,''), COALESCE(backdrop,''),
		COALESCE(overview,''), COALESCE(tagline,''), COALESCE(release_date,''),
		COALESCE(synced_at, CURRENT_TIMESTAMP)
		FROM viewings WHERE 1=1`
	args := []any{}
	if user != "" {
		q += ` AND user=?`
		args = append(args, user)
	}
	if fromDate != "" {
		q += ` AND watched_date>=?`
		args = append(args, fromDate)
	}
	if toDate != "" {
		q += ` AND watched_date<=?`
		args = append(args, toDate)
	}
	q += ` ORDER BY watched_date DESC, viewing_id DESC`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Viewing
	for rows.Next() {
		var v Viewing
		var liked, rewatch, hasReview int
		var syncedAt string
		if err := rows.Scan(&v.ViewingID, &v.User, &v.WatchedDate, &v.Slug, &v.Title,
			&v.Year, &v.Rating, &liked, &rewatch, &hasReview,
			&v.TMDBID, &v.TMDBTitle, &v.Director, &v.Runtime, &v.Genres, &v.TMDBRating,
			&v.Poster, &v.PosterLg, &v.Backdrop, &v.Overview, &v.Tagline, &v.ReleaseDate,
			&syncedAt); err != nil {
			return nil, err
		}
		v.Liked = liked != 0
		v.Rewatch = rewatch != 0
		v.HasReview = hasReview != 0
		// SQLite returns the timestamp as a string; tolerate either format.
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
			if t, err := time.Parse(layout, syncedAt); err == nil {
				v.SyncedAt = t
				break
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// FindCachedTMDBBySlug returns a previously-enriched viewing for the given
// film slug, so a sync can reuse cached TMDB metadata instead of re-querying.
func (s *Store) FindCachedTMDBBySlug(slug string) (*Viewing, bool) {
	if slug == "" {
		return nil, false
	}
	row := s.db.QueryRow(`SELECT
		COALESCE(tmdb_id,0), COALESCE(tmdb_title,''), COALESCE(director,''),
		COALESCE(runtime,0), COALESCE(genres,''), COALESCE(tmdb_rating,0),
		COALESCE(poster,''), COALESCE(poster_lg,''), COALESCE(backdrop,''),
		COALESCE(overview,''), COALESCE(tagline,''), COALESCE(release_date,'')
		FROM viewings WHERE film_slug=? AND tmdb_id IS NOT NULL AND tmdb_id != 0
		ORDER BY synced_at DESC LIMIT 1`, slug)
	var v Viewing
	if err := row.Scan(&v.TMDBID, &v.TMDBTitle, &v.Director, &v.Runtime, &v.Genres,
		&v.TMDBRating, &v.Poster, &v.PosterLg, &v.Backdrop, &v.Overview,
		&v.Tagline, &v.ReleaseDate); err != nil {
		return nil, false
	}
	return &v, v.TMDBID != 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullableFloat returns nil for 0 so SQLite stores NULL (preserves "unrated").
func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}
