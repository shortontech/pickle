//go:build ignore

package models

// ActionType represents a row in the action_types table.
// Append-only: no Update or Delete methods.
type ActionType struct {
	ID          int    `json:"id" db:"id"`
	ModelTypeID int    `json:"model_type_id" db:"model_type_id"`
	Name        string `json:"name" db:"name"`
}
