package store

import (
	"database/sql"
	"fmt"
)

// CreateVoter registers a fellow traveller for a trip and returns their cookie
// token.
func (s *Store) CreateVoter(tripID int64, name string) (*Voter, error) {
	v := Voter{TripID: tripID, Name: name, Token: newToken(12), CreatedAt: now()}
	res, err := s.db.Exec(
		`INSERT INTO voters (trip_id, name, token, created_at) VALUES (?, ?, ?, ?)`,
		v.TripID, v.Name, v.Token, v.CreatedAt)
	if err != nil {
		return nil, err
	}
	v.ID, err = res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VoterByToken loads a voter for a trip by their cookie token.
func (s *Store) VoterByToken(tripID int64, token string) (*Voter, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	var v Voter
	err := s.db.QueryRow(
		`SELECT id, trip_id, name, token, created_at FROM voters WHERE trip_id = ? AND token = ?`, tripID, token).
		Scan(&v.ID, &v.TripID, &v.Name, &v.Token, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// targetKey builds the map key used for vote lookups.
func targetKey(targetType string, targetID int64) string {
	return fmt.Sprintf("%s:%d", targetType, targetID)
}

// ToggleVote adds the voter's upvote on a target, or removes it if present.
func (s *Store) ToggleVote(tripID, voterID int64, targetType string, targetID int64) error {
	var existing int64
	err := s.db.QueryRow(
		`SELECT id FROM votes WHERE voter_id = ? AND target_type = ? AND target_id = ?`,
		voterID, targetType, targetID).Scan(&existing)
	if err == nil {
		_, derr := s.db.Exec(`DELETE FROM votes WHERE id = ?`, existing)
		return derr
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO votes (trip_id, voter_id, target_type, target_id, value, created_at) VALUES (?, ?, ?, ?, 1, ?)`,
		tripID, voterID, targetType, targetID, now())
	return err
}

// VoteCounts returns total vote value per target for a trip, keyed "type:id".
func (s *Store) VoteCounts(tripID int64) (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT target_type, target_id, COALESCE(SUM(value),0) FROM votes WHERE trip_id = ? GROUP BY target_type, target_id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var t string
		var id int64
		var n int
		if err := rows.Scan(&t, &id, &n); err != nil {
			return nil, err
		}
		out[targetKey(t, id)] = n
	}
	return out, rows.Err()
}

// VotesByVoter returns the set of targets a voter has upvoted, keyed "type:id".
func (s *Store) VotesByVoter(voterID int64) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT target_type, target_id FROM votes WHERE voter_id = ?`, voterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var t string
		var id int64
		if err := rows.Scan(&t, &id); err != nil {
			return nil, err
		}
		out[targetKey(t, id)] = true
	}
	return out, rows.Err()
}

// VoterActivityPoints returns a voter's per-activity rank points (Borda points,
// higher = more preferred), keyed by list_item id.
func (s *Store) VoterActivityPoints(voterID int64) (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT target_id, value FROM votes WHERE voter_id = ? AND target_type = 'list_item'`, voterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var v int
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// SetVoterActivityPoints replaces a voter's activity ranking with the given
// points. These sum across voters (via VoteCounts) into the weighted ranking.
func (s *Store) SetVoterActivityPoints(tripID, voterID int64, points map[int64]int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM votes WHERE voter_id = ? AND target_type = 'list_item'`, voterID); err != nil {
		return err
	}
	ts := now()
	for id, p := range points {
		if p <= 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO votes (trip_id, voter_id, target_type, target_id, value, created_at) VALUES (?, ?, 'list_item', ?, ?, ?)`,
			tripID, voterID, id, p, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}
