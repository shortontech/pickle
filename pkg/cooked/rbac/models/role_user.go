//go:build ignore

package models

import (
	"github.com/google/uuid"
	"time"
)

// RoleUser represents a row in the role_user pivot table.
type RoleUser struct {
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	RoleID    uuid.UUID `json:"role_id" db:"role_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
