//go:build ignore

package models

import (
	"github.com/google/uuid"
	"time"
)

// UserAction represents a row in the user_actions table.
// Append-only: no Update or Delete methods.
type UserAction struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	UserID            uuid.UUID  `json:"user_id" db:"user_id"`
	ActionTypeID      int        `json:"action_type_id" db:"action_type_id"`
	ResourceID        uuid.UUID  `json:"resource_id" db:"resource_id"`
	ResourceVersionID *uuid.UUID `json:"resource_version_id" db:"resource_version_id"`
	RoleID            *uuid.UUID `json:"role_id" db:"role_id"`
	IPAddress         *string    `json:"ip_address" db:"ip_address"`
	RequestID         *string    `json:"request_id" db:"request_id"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
}
