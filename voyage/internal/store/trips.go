package store

import "database/sql"

// CreateTrip inserts a trip, seeds its budget and dates axes plus an Activities
// list, and attaches any initial locations. Returns the new trip id.
func (s *Store) CreateTrip(title string, locationNames []string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	ts := now()
	res, err := tx.Exec(
		`INSERT INTO trips (title, stage, share_token, created_at, updated_at) VALUES (?, 'ideate', ?, ?, ?)`,
		title, newToken(12), ts, ts,
	)
	if err != nil {
		return 0, err
	}
	tripID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Seed the two default axes used by the v1 budget × dates matrix.
	if _, err := tx.Exec(`INSERT INTO axes (trip_id, name, kind, position) VALUES (?, 'Budget', 'budget', 0)`, tripID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO axes (trip_id, name, kind, position) VALUES (?, 'Dates', 'date_range', 1)`, tripID); err != nil {
		return 0, err
	}
	// Seed the default Activities list.
	if _, err := tx.Exec(`INSERT INTO lists (trip_id, name, kind, position) VALUES (?, 'Activities', 'activity', 0)`, tripID); err != nil {
		return 0, err
	}

	for i, name := range locationNames {
		if name == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO locations (trip_id, name, position) VALUES (?, ?, ?)`, tripID, name, i); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return tripID, nil
}

// ListTrips returns dashboard summaries, newest first.
func (s *Store) ListTrips() ([]TripSummary, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.title, t.stage, t.created_at,
			(SELECT COUNT(*) FROM combos c WHERE c.trip_id = t.id
				AND EXISTS (SELECT 1 FROM combo_items ci WHERE ci.combo_id = c.id)) AS scenarios,
			(SELECT COUNT(*) FROM list_items li
				JOIN lists l ON l.id = li.list_id WHERE l.trip_id = t.id) AS activities
		FROM trips t
		ORDER BY t.created_at DESC, t.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TripSummary
	for rows.Next() {
		var t TripSummary
		if err := rows.Scan(&t.ID, &t.Title, &t.Stage, &t.CreatedAt, &t.Scenarios, &t.Activities); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTrip loads a single trip by id.
func (s *Store) GetTrip(id int64) (*Trip, error) {
	return s.scanTrip(s.db.QueryRow(
		`SELECT id, title, stage, share_token, notes, party_size, created_at, updated_at FROM trips WHERE id = ?`, id))
}

// GetTripByToken loads a trip by its share token.
func (s *Store) GetTripByToken(token string) (*Trip, error) {
	return s.scanTrip(s.db.QueryRow(
		`SELECT id, title, stage, share_token, notes, party_size, created_at, updated_at FROM trips WHERE share_token = ?`, token))
}

func (s *Store) scanTrip(row *sql.Row) (*Trip, error) {
	var t Trip
	err := row.Scan(&t.ID, &t.Title, &t.Stage, &t.ShareToken, &t.Notes, &t.PartySize, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// SetPartySize updates the number of travellers (used for per-person totals).
func (s *Store) SetPartySize(id int64, n int) error {
	if n < 1 {
		n = 1
	}
	_, err := s.db.Exec(`UPDATE trips SET party_size = ?, updated_at = ? WHERE id = ?`, n, now(), id)
	return err
}

// DeleteTrip removes a trip and (via cascade) everything under it.
func (s *Store) DeleteTrip(id int64) error {
	_, err := s.db.Exec(`DELETE FROM trips WHERE id = ?`, id)
	return err
}

// RotateShareToken assigns a fresh share token, invalidating old links.
func (s *Store) RotateShareToken(id int64) (string, error) {
	tok := newToken(12)
	if _, err := s.db.Exec(`UPDATE trips SET share_token = ?, updated_at = ? WHERE id = ?`, tok, now(), id); err != nil {
		return "", err
	}
	return tok, nil
}

// SetStage updates a trip's lifecycle stage.
func (s *Store) SetStage(id int64, stage string) error {
	_, err := s.db.Exec(`UPDATE trips SET stage = ?, updated_at = ? WHERE id = ?`, stage, now(), id)
	return err
}

// LocationsForTrip returns a trip's locations in order.
func (s *Store) LocationsForTrip(tripID int64) ([]Location, error) {
	rows, err := s.db.Query(
		`SELECT id, trip_id, name, position, arrive, depart, notes FROM locations WHERE trip_id = ? ORDER BY position, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Location
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.ID, &l.TripID, &l.Name, &l.Position, &l.Arrive, &l.Depart, &l.Notes); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AddLocation appends a location to a trip.
func (s *Store) AddLocation(tripID int64, name string) error {
	var pos int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM locations WHERE trip_id = ?`, tripID).Scan(&pos)
	_, err := s.db.Exec(`INSERT INTO locations (trip_id, name, position) VALUES (?, ?, ?)`, tripID, name, pos)
	return err
}

// DeleteLocation removes a location.
func (s *Store) DeleteLocation(id int64) error {
	_, err := s.db.Exec(`DELETE FROM locations WHERE id = ?`, id)
	return err
}
