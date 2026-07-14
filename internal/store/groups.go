package store

import (
	"database/sql"
	"fmt"

	"todo/internal/models"
)

// ListGroups returns all groups ordered by sort_order then id.
func ListGroups(db *sql.DB) ([]models.Group, error) {
	rows, err := db.Query(`SELECT id, name, color, sort_order, created_at
		FROM groups ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gs []models.Group
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Color, &g.SortOrder, &g.CreatedAt); err != nil {
			return nil, err
		}
		gs = append(gs, g)
	}
	return gs, rows.Err()
}

// GetGroup fetches a single group by id.
func GetGroup(db *sql.DB, id int64) (models.Group, error) {
	var g models.Group
	err := db.QueryRow(`SELECT id, name, color, sort_order, created_at
		FROM groups WHERE id = ?`, id).
		Scan(&g.ID, &g.Name, &g.Color, &g.SortOrder, &g.CreatedAt)
	return g, err
}

// CreateGroup inserts a group and returns it with the new id.
func CreateGroup(db *sql.DB, g models.Group) (models.Group, error) {
	if g.Name == "" {
		return models.Group{}, fmt.Errorf("name is required")
	}
	if g.Color == "" {
		g.Color = "#6b7280"
	}
	res, err := db.Exec(`INSERT INTO groups (name, color, sort_order) VALUES (?, ?, ?)`,
		g.Name, g.Color, g.SortOrder)
	if err != nil {
		return models.Group{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Group{}, err
	}
	return GetGroup(db, id)
}

// UpdateGroup updates name/color/sort_order for the given id.
func UpdateGroup(db *sql.DB, id int64, g models.Group) (models.Group, error) {
	if _, err := db.Exec(`UPDATE groups SET name = ?, color = ?, sort_order = ? WHERE id = ?`,
		g.Name, g.Color, g.SortOrder, id); err != nil {
		return models.Group{}, err
	}
	return GetGroup(db, id)
}

// DeleteGroup removes a group. Todos/time_entries referencing it get NULL
// (via ON DELETE SET NULL).
func DeleteGroup(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	return err
}
