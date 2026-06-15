package store

import "database/sql"

// ListsForTrip returns a trip's lists with their items, ordered by rank.
func (s *Store) ListsForTrip(tripID int64) ([]List, error) {
	rows, err := s.db.Query(`SELECT id, trip_id, name, kind, position FROM lists WHERE trip_id = ? ORDER BY position, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []List
	for rows.Next() {
		var l List
		if err := rows.Scan(&l.ID, &l.TripID, &l.Name, &l.Kind, &l.Position); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range lists {
		items, err := s.listItems(lists[i].ID)
		if err != nil {
			return nil, err
		}
		lists[i].Items = items
	}
	return lists, nil
}

func (s *Store) listItems(listID int64) ([]ListItem, error) {
	rows, err := s.db.Query(
		`SELECT id, list_id, label, position, notes, link, COALESCE(location_id, 0), metadata FROM list_items WHERE list_id = ? ORDER BY position, id`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ListItem
	for rows.Next() {
		var it ListItem
		var meta string
		if err := rows.Scan(&it.ID, &it.ListID, &it.Label, &it.Position, &it.Notes, &it.Link, &it.LocationID, &meta); err != nil {
			return nil, err
		}
		it.Meta = unmarshalMeta(meta)
		out = append(out, it)
	}
	return out, rows.Err()
}

// ActivitiesList returns the trip's seeded activity list (creating it if a legacy
// trip somehow lacks one).
func (s *Store) ActivitiesList(tripID int64) (*List, error) {
	var l List
	err := s.db.QueryRow(`SELECT id, trip_id, name, kind, position FROM lists WHERE trip_id = ? AND kind = 'activity' ORDER BY position, id LIMIT 1`, tripID).
		Scan(&l.ID, &l.TripID, &l.Name, &l.Kind, &l.Position)
	if err == sql.ErrNoRows {
		res, err := s.db.Exec(`INSERT INTO lists (trip_id, name, kind, position) VALUES (?, 'Activities', 'activity', 0)`, tripID)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		return &List{ID: id, TripID: tripID, Name: "Activities", Kind: "activity"}, nil
	}
	if err != nil {
		return nil, err
	}
	items, err := s.listItems(l.ID)
	if err != nil {
		return nil, err
	}
	l.Items = items
	return &l, nil
}

// AddListItem appends an item to a list.
func (s *Store) AddListItem(listID int64, label, notes, link string) (int64, error) {
	var pos int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM list_items WHERE list_id = ?`, listID).Scan(&pos)
	res, err := s.db.Exec(
		`INSERT INTO list_items (list_id, label, position, notes, link) VALUES (?, ?, ?, ?, ?)`,
		listID, label, pos, notes, link)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteListItem removes an item.
func (s *Store) DeleteListItem(id int64) error {
	_, err := s.db.Exec(`DELETE FROM list_items WHERE id = ?`, id)
	return err
}

// MoveListItem swaps an item's manual rank with its neighbour. dir is "up" or
// "down".
func (s *Store) MoveListItem(id int64, dir string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var listID int64
	var pos int
	if err := tx.QueryRow(`SELECT list_id, position FROM list_items WHERE id = ?`, id).Scan(&listID, &pos); err != nil {
		return err
	}

	var neighbourID int64
	var neighbourPos int
	var q string
	if dir == "up" {
		q = `SELECT id, position FROM list_items WHERE list_id = ? AND position < ? ORDER BY position DESC LIMIT 1`
	} else {
		q = `SELECT id, position FROM list_items WHERE list_id = ? AND position > ? ORDER BY position ASC LIMIT 1`
	}
	err = tx.QueryRow(q, listID, pos).Scan(&neighbourID, &neighbourPos)
	if err == sql.ErrNoRows {
		return nil // already at the end; nothing to do
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`UPDATE list_items SET position = ? WHERE id = ?`, neighbourPos, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE list_items SET position = ? WHERE id = ?`, pos, neighbourID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListItemTripID returns the trip an item belongs to (for redirects).
func (s *Store) ListItemTripID(itemID int64) (int64, error) {
	var tripID int64
	err := s.db.QueryRow(
		`SELECT l.trip_id FROM list_items li JOIN lists l ON l.id = li.list_id WHERE li.id = ?`, itemID).Scan(&tripID)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return tripID, err
}
