package store

import (
	"database/sql"
	"fmt"

	"todo/internal/models"
)

// ListGroups returns all tags ordered by id.
func ListGroups(db *sql.DB) ([]models.Group, error) {
	rows, err := db.Query(`SELECT id, name, description, color, include_in_stats, created_at
		FROM tags ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gs []models.Group
	for rows.Next() {
		var g models.Group
		var include bool
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Color, &include, &g.CreatedAt); err != nil {
			return nil, err
		}
		g.IncludeInStats = &include
		gs = append(gs, g)
	}
	return gs, rows.Err()
}

// GetGroup fetches a single group by id.
func GetGroup(db *sql.DB, id int64) (models.Group, error) {
	var g models.Group
	var include bool
	err := db.QueryRow(`SELECT id, name, description, color, include_in_stats, created_at
		FROM tags WHERE id = ?`, id).
		Scan(&g.ID, &g.Name, &g.Description, &g.Color, &include, &g.CreatedAt)
	g.IncludeInStats = &include
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
	include := true
	if g.IncludeInStats != nil {
		include = *g.IncludeInStats
	}
	res, err := db.Exec(`INSERT INTO tags (name, description, color, include_in_stats) VALUES (?, ?, ?, ?)`, g.Name, g.Description, g.Color, include)
	if err != nil {
		return models.Group{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Group{}, err
	}
	return GetGroup(db, id)
}

// UpdateGroup updates a tag's editable fields.
func UpdateGroup(db *sql.DB, id int64, g models.Group) (models.Group, error) {
	if g.Name == "" {
		return models.Group{}, fmt.Errorf("name is required")
	}
	if g.IncludeInStats == nil {
		existing, err := GetGroup(db, id)
		if err != nil {
			return models.Group{}, err
		}
		g.IncludeInStats = existing.IncludeInStats
	}
	if _, err := db.Exec(`UPDATE tags SET name = ?, description = ?, color = ?, include_in_stats = ? WHERE id = ?`, g.Name, g.Description, g.Color, *g.IncludeInStats, id); err != nil {
		return models.Group{}, err
	}
	return GetGroup(db, id)
}

// DeleteGroup removes a group. Todos/time_entries referencing it get NULL
// (via ON DELETE SET NULL).
func DeleteGroup(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM tags WHERE id = ?`, id)
	return err
}
