//go:build ignore

package models

// ModelType represents a row in the model_types table.
// Append-only: no Update or Delete methods.
type ModelType struct {
	ID   int    `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}
