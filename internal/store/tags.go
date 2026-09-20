package store

import (
	"database/sql"
	"fmt"

	"todo/internal/models"
)

// ListTags returns all tags ordered by id.
func ListTags(db *sql.DB) ([]models.Tag, error) {
	rows, err := db.Query(`SELECT id, name, description, color, include_in_stats, created_at
		FROM tags ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []models.Tag
	for rows.Next() {
		var tag models.Tag
		var include bool
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Description, &tag.Color, &include, &tag.CreatedAt); err != nil {
			return nil, err
		}
		tag.IncludeInStats = &include
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// GetTag fetches a single tag by id.
func GetTag(db *sql.DB, id int64) (models.Tag, error) {
	var tag models.Tag
	var include bool
	err := db.QueryRow(`SELECT id, name, description, color, include_in_stats, created_at
		FROM tags WHERE id = ?`, id).
		Scan(&tag.ID, &tag.Name, &tag.Description, &tag.Color, &include, &tag.CreatedAt)
	tag.IncludeInStats = &include
	return tag, err
}

// CreateTag inserts a tag and returns it with the new id.
func CreateTag(db *sql.DB, g models.Tag) (models.Tag, error) {
	if g.Name == "" {
		return models.Tag{}, fmt.Errorf("name is required")
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
		return models.Tag{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return models.Tag{}, err
	}
	return GetTag(db, id)
}

// UpdateTag updates a tag's editable fields.
func UpdateTag(db *sql.DB, id int64, g models.Tag) (models.Tag, error) {
	if g.Name == "" {
		return models.Tag{}, fmt.Errorf("name is required")
	}
	if g.IncludeInStats == nil {
		existing, err := GetTag(db, id)
		if err != nil {
			return models.Tag{}, err
		}
		g.IncludeInStats = existing.IncludeInStats
	}
	if _, err := db.Exec(`UPDATE tags SET name = ?, description = ?, color = ?, include_in_stats = ? WHERE id = ?`, g.Name, g.Description, g.Color, *g.IncludeInStats, id); err != nil {
		return models.Tag{}, err
	}
	return GetTag(db, id)
}

// DeleteTag removes a tag. Todos/time_entries referencing it get NULL
// (via ON DELETE SET NULL).
func DeleteTag(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM tags WHERE id = ?`, id)
	return err
}
