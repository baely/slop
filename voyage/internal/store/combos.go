package store

import "database/sql"

// EnsureCombo returns the id of the combo for a trip whose option tuple exactly
// matches sel (axisID -> axisOptionID), creating it if necessary. This makes the
// budget × dates matrix lazy: a combo only materialises when its first hotel is
// added, avoiding a cartesian blow-up of empty cells.
func (s *Store) EnsureCombo(tripID int64, sel map[int64]int64) (int64, error) {
	if len(sel) == 0 {
		return 0, ErrNotFound
	}
	if id, err := s.findCombo(tripID, sel); err != nil {
		return 0, err
	} else if id != 0 {
		return id, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	var pos int
	_ = tx.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM combos WHERE trip_id = ?`, tripID).Scan(&pos)
	res, err := tx.Exec(`INSERT INTO combos (trip_id, position) VALUES (?, ?)`, tripID, pos)
	if err != nil {
		return 0, err
	}
	comboID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for axisID, optID := range sel {
		if _, err := tx.Exec(
			`INSERT INTO combo_options (combo_id, axis_id, axis_option_id) VALUES (?, ?, ?)`,
			comboID, axisID, optID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return comboID, nil
}

// findCombo returns the id of a combo whose tuple matches sel exactly, or 0.
func (s *Store) findCombo(tripID int64, sel map[int64]int64) (int64, error) {
	rows, err := s.db.Query(`
		SELECT co.combo_id, co.axis_id, co.axis_option_id
		FROM combo_options co JOIN combos c ON c.id = co.combo_id
		WHERE c.trip_id = ?`, tripID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tuples := map[int64]map[int64]int64{}
	for rows.Next() {
		var comboID, axisID, optID int64
		if err := rows.Scan(&comboID, &axisID, &optID); err != nil {
			return 0, err
		}
		if tuples[comboID] == nil {
			tuples[comboID] = map[int64]int64{}
		}
		tuples[comboID][axisID] = optID
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for comboID, t := range tuples {
		if len(t) != len(sel) {
			continue
		}
		match := true
		for axisID, optID := range sel {
			if t[axisID] != optID {
				match = false
				break
			}
		}
		if match {
			return comboID, nil
		}
	}
	return 0, nil
}

// CombosForTrip returns all combos for a trip with their options and items.
func (s *Store) CombosForTrip(tripID int64) ([]Combo, error) {
	rows, err := s.db.Query(`SELECT id, trip_id, label, position, status FROM combos WHERE trip_id = ? ORDER BY position, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var combos []Combo
	for rows.Next() {
		var c Combo
		if err := rows.Scan(&c.ID, &c.TripID, &c.Label, &c.Position, &c.Status); err != nil {
			return nil, err
		}
		combos = append(combos, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range combos {
		opts, err := s.comboOptions(combos[i].ID)
		if err != nil {
			return nil, err
		}
		combos[i].Options = opts
		items, err := s.comboItems(combos[i].ID)
		if err != nil {
			return nil, err
		}
		combos[i].Items = items
	}
	return combos, nil
}

// ComboByID loads one combo with options and items.
func (s *Store) ComboByID(id int64) (*Combo, error) {
	var c Combo
	err := s.db.QueryRow(`SELECT id, trip_id, label, position, status FROM combos WHERE id = ?`, id).
		Scan(&c.ID, &c.TripID, &c.Label, &c.Position, &c.Status)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Options, err = s.comboOptions(id); err != nil {
		return nil, err
	}
	if c.Items, err = s.comboItems(id); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) comboOptions(comboID int64) ([]ComboOption, error) {
	rows, err := s.db.Query(`
		SELECT co.axis_id, a.name, a.kind, co.axis_option_id, o.label
		FROM combo_options co
		JOIN axes a ON a.id = co.axis_id
		JOIN axis_options o ON o.id = co.axis_option_id
		WHERE co.combo_id = ?
		ORDER BY a.position, a.id`, comboID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ComboOption
	for rows.Next() {
		var o ComboOption
		if err := rows.Scan(&o.AxisID, &o.AxisName, &o.AxisKind, &o.AxisOptionID, &o.OptionLabel); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) comboItems(comboID int64) ([]ComboItem, error) {
	rows, err := s.db.Query(
		`SELECT id, combo_id, category, label, position, status, link, notes, metadata FROM combo_items WHERE combo_id = ? ORDER BY position, id`, comboID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ComboItem
	for rows.Next() {
		var it ComboItem
		var meta string
		if err := rows.Scan(&it.ID, &it.ComboID, &it.Category, &it.Label, &it.Position, &it.Status, &it.Link, &it.Notes, &meta); err != nil {
			return nil, err
		}
		it.Meta = unmarshalMeta(meta)
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetComboStatus updates a combo's shortlist/selection status.
func (s *Store) SetComboStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE combos SET status = ? WHERE id = ?`, status, id)
	return err
}

// ComboTripID returns the trip a combo belongs to (for redirects).
func (s *Store) ComboTripID(comboID int64) (int64, error) {
	var tripID int64
	err := s.db.QueryRow(`SELECT trip_id FROM combos WHERE id = ?`, comboID).Scan(&tripID)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return tripID, err
}

// AddComboItem appends a dependent option (a hotel) to a combo. createdBy is the
// suggesting voter (nil for the organiser).
func (s *Store) AddComboItem(comboID int64, category, label, link, notes string, meta map[string]any, createdBy *int64) (int64, error) {
	if category == "" {
		category = "hotel"
	}
	var pos int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM combo_items WHERE combo_id = ?`, comboID).Scan(&pos)
	res, err := s.db.Exec(
		`INSERT INTO combo_items (combo_id, category, label, position, link, notes, metadata, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		comboID, category, label, pos, link, notes, marshalMeta(meta), nullableID(createdBy))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CountComboItemsBy returns how many items on a combo were suggested by a voter.
func (s *Store) CountComboItemsBy(comboID, voterID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM combo_items WHERE combo_id = ? AND created_by = ?`, comboID, voterID).Scan(&n)
	return n, err
}

// DeleteComboItem removes a combo item.
func (s *Store) DeleteComboItem(id int64) error {
	_, err := s.db.Exec(`DELETE FROM combo_items WHERE id = ?`, id)
	return err
}

// SetComboItemStatus sets an item's status. Promoting one item to "selected"
// demotes any other selected sibling back to "option" (one pick per combo).
func (s *Store) SetComboItemStatus(id int64, status string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var comboID int64
	if err := tx.QueryRow(`SELECT combo_id FROM combo_items WHERE id = ?`, id).Scan(&comboID); err != nil {
		return err
	}
	if status == "selected" {
		if _, err := tx.Exec(`UPDATE combo_items SET status = 'option' WHERE combo_id = ? AND status = 'selected'`, comboID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE combo_items SET status = ? WHERE id = ?`, status, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ComboItemComboID returns the combo an item belongs to (for redirects).
func (s *Store) ComboItemComboID(itemID int64) (int64, error) {
	var comboID int64
	err := s.db.QueryRow(`SELECT combo_id FROM combo_items WHERE id = ?`, itemID).Scan(&comboID)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return comboID, err
}
