package store

import "database/sql"

// AxesForTrip returns a trip's axes (with their options) in order.
func (s *Store) AxesForTrip(tripID int64) ([]Axis, error) {
	rows, err := s.db.Query(`SELECT id, trip_id, name, kind, position FROM axes WHERE trip_id = ? ORDER BY position, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var axes []Axis
	for rows.Next() {
		var a Axis
		if err := rows.Scan(&a.ID, &a.TripID, &a.Name, &a.Kind, &a.Position); err != nil {
			return nil, err
		}
		axes = append(axes, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range axes {
		opts, err := s.optionsForAxis(axes[i].ID)
		if err != nil {
			return nil, err
		}
		axes[i].Options = opts
	}
	return axes, nil
}

func (s *Store) optionsForAxis(axisID int64) ([]AxisOption, error) {
	rows, err := s.db.Query(`SELECT id, axis_id, label, position, metadata FROM axis_options WHERE axis_id = ? ORDER BY position, id`, axisID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AxisOption
	for rows.Next() {
		var o AxisOption
		var meta string
		if err := rows.Scan(&o.ID, &o.AxisID, &o.Label, &o.Position, &meta); err != nil {
			return nil, err
		}
		o.Meta = unmarshalMeta(meta)
		out = append(out, o)
	}
	return out, rows.Err()
}

// AxisByID loads a single axis (without options).
func (s *Store) AxisByID(id int64) (*Axis, error) {
	var a Axis
	err := s.db.QueryRow(`SELECT id, trip_id, name, kind, position FROM axes WHERE id = ?`, id).
		Scan(&a.ID, &a.TripID, &a.Name, &a.Kind, &a.Position)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AddAxis creates a generic axis on a trip (future option dimensions).
func (s *Store) AddAxis(tripID int64, name, kind string) (int64, error) {
	var pos int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM axes WHERE trip_id = ?`, tripID).Scan(&pos)
	res, err := s.db.Exec(`INSERT INTO axes (trip_id, name, kind, position) VALUES (?, ?, ?, ?)`, tripID, name, kind, pos)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AddAxisOption appends an option to an axis.
func (s *Store) AddAxisOption(axisID int64, label string, meta map[string]any) (int64, error) {
	var pos int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(position)+1, 0) FROM axis_options WHERE axis_id = ?`, axisID).Scan(&pos)
	res, err := s.db.Exec(
		`INSERT INTO axis_options (axis_id, label, position, metadata) VALUES (?, ?, ?, ?)`,
		axisID, label, pos, marshalMeta(meta))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteAxisOption removes an option (and, via cascade, any combos that used it).
func (s *Store) DeleteAxisOption(id int64) error {
	// Combos referencing this option become incomplete tuples, so drop them too.
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(`DELETE FROM combos WHERE id IN (SELECT combo_id FROM combo_options WHERE axis_option_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM axis_options WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// AxisOptionTripID returns the trip an axis option belongs to (for redirects).
func (s *Store) AxisOptionTripID(optionID int64) (int64, error) {
	var tripID int64
	err := s.db.QueryRow(
		`SELECT a.trip_id FROM axis_options o JOIN axes a ON a.id = o.axis_id WHERE o.id = ?`, optionID).Scan(&tripID)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return tripID, err
}

// AxisOptionAxisID returns the axis an option belongs to (used to group hotels
// under a budget option).
func (s *Store) AxisOptionAxisID(optionID int64) (int64, error) {
	var axisID int64
	err := s.db.QueryRow(`SELECT axis_id FROM axis_options WHERE id = ?`, optionID).Scan(&axisID)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	return axisID, err
}
