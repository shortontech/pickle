//go:build ignore

package models

import (
	"github.com/google/uuid"
	"time"
)

// Role represents a row in the roles table.
type Role struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Slug        string    `json:"slug" db:"slug"`
	Name        string    `json:"name" db:"name"`
	Manages     bool      `json:"manages" db:"manages"`
	IsDefault   bool      `json:"is_default" db:"is_default"`
	BirthPolicy string    `json:"birth_policy" db:"birth_policy"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}
