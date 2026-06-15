// Package store is Voyage's SQLite persistence layer. It models trips and a set
// of deliberately generic "spines" so the planner can grow without schema churn:
//
//   - Options spine: axes -> axis_options -> combos (tuples of options) ->
//     combo_items (the dependent options, e.g. hotels). v1 uses two axes
//     (budget, dates) and one item category (hotel), but the model is N-axis and
//     multi-category.
//   - Lists spine: lists -> list_items, a generic rankable list. v1 seeds one
//     "Activities" list per trip.
//   - Voting spine: voters, votes and comments keyed by a generic
//     (target_type, target_id) pair, so anything can become votable later.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite database backing Voyage.
type Store struct {
	db *sql.DB
}

// Open opens (and pings) the SQLite database at path.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("db path is empty")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite + WAL is happiest with serialized writes.
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Migrate creates the schema if it does not already exist.
func (s *Store) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS trips (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			stage TEXT NOT NULL DEFAULT 'ideate',
			share_token TEXT NOT NULL UNIQUE,
			notes TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS locations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			arrive TEXT NOT NULL DEFAULT '',
			depart TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS axes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'generic',
			position INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS axis_options (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			axis_id INTEGER NOT NULL REFERENCES axes(id) ON DELETE CASCADE,
			label TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			metadata TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS combos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			label TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'candidate'
		);`,
		`CREATE TABLE IF NOT EXISTS combo_options (
			combo_id INTEGER NOT NULL REFERENCES combos(id) ON DELETE CASCADE,
			axis_id INTEGER NOT NULL REFERENCES axes(id) ON DELETE CASCADE,
			axis_option_id INTEGER NOT NULL REFERENCES axis_options(id) ON DELETE CASCADE,
			PRIMARY KEY (combo_id, axis_id)
		);`,
		`CREATE TABLE IF NOT EXISTS combo_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			combo_id INTEGER NOT NULL REFERENCES combos(id) ON DELETE CASCADE,
			category TEXT NOT NULL DEFAULT 'hotel',
			label TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'option',
			link TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			metadata TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS lists (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'generic',
			position INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS list_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			list_id INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
			label TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			notes TEXT NOT NULL DEFAULT '',
			link TEXT NOT NULL DEFAULT '',
			location_id INTEGER REFERENCES locations(id) ON DELETE SET NULL,
			metadata TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS voters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS votes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			voter_id INTEGER NOT NULL REFERENCES voters(id) ON DELETE CASCADE,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			value INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			UNIQUE(voter_id, target_type, target_id)
		);`,
		`CREATE TABLE IF NOT EXISTS comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
			voter_id INTEGER REFERENCES voters(id) ON DELETE SET NULL,
			target_type TEXT NOT NULL,
			target_id INTEGER NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Additive migrations for existing databases.
	if err := s.addColumnIfMissing("trips", "party_size", "party_size INTEGER NOT NULL DEFAULT 2"); err != nil {
		return fmt.Errorf("migrate party_size: %w", err)
	}
	return nil
}

// addColumnIfMissing adds a column to an existing table when it is not already
// present, so persisted databases pick up new fields without losing data.
func (s *Store) addColumnIfMissing(table, column, ddl string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + ddl)
	return err
}

// now returns the current time as an RFC3339 string for storage.
func now() string { return time.Now().UTC().Format(time.RFC3339) }

// newToken returns a URL-safe random hex token of n bytes (2n hex chars).
func newToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is fatal-level; fall back to a time seed so we
		// never hand out an empty token.
		return hex.EncodeToString([]byte(now()))
	}
	return hex.EncodeToString(b)
}

// marshalMeta encodes a metadata map to JSON for storage. Empty maps become "".
func marshalMeta(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// unmarshalMeta decodes stored metadata JSON into a map for template use.
func unmarshalMeta(s string) map[string]any {
	m := map[string]any{}
	if s == "" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}
