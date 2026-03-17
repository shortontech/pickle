package schema

import "strings"

// singularize does a best-effort conversion of a plural table name to singular.
// Handles common English plurals. Used internally by flattenRelationships to
// derive FK column names (e.g., "users" → "user_id").
func singularize(name string) string {
	// Irregular plurals
	irregulars := map[string]string{
		"people":   "person",
		"children": "child",
		"men":      "man",
		"women":    "woman",
		"mice":     "mouse",
		"geese":    "goose",
		"teeth":    "tooth",
		"feet":     "foot",
		"data":     "datum",
		"indices":  "index",
		"matrices": "matrix",
	}
	lower := strings.ToLower(name)
	if s, ok := irregulars[lower]; ok {
		return s
	}

	// Common suffix rules (order matters)
	if strings.HasSuffix(lower, "ies") && len(lower) > 3 {
		return lower[:len(lower)-3] + "y"
	}
	if strings.HasSuffix(lower, "ses") || strings.HasSuffix(lower, "xes") || strings.HasSuffix(lower, "zes") || strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "shes") {
		if strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "shes") {
			return lower[:len(lower)-2]
		}
		return lower[:len(lower)-2]
	}
	if strings.HasSuffix(lower, "sses") {
		return lower[:len(lower)-2]
	}
	if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") {
		return lower[:len(lower)-1]
	}
	return lower
}

// findPKName returns the name of the primary key column, defaulting to "id".
func findPKName(t *Table) string {
	for _, col := range t.Columns {
		if col.IsPrimaryKey {
			return col.Name
		}
	}
	return "id"
}

// findPKType returns the ColumnType of the primary key, defaulting to UUID.
func findPKType(t *Table) ColumnType {
	for _, col := range t.Columns {
		if col.IsPrimaryKey {
			return col.Type
		}
	}
	return UUID
}

// insertFKColumn inserts the FK column after the primary key column.
// If no PK is found, it prepends the FK column.
func insertFKColumn(cols []*Column, fk *Column) []*Column {
	for i, col := range cols {
		if col.IsPrimaryKey {
			// Insert after PK
			result := make([]*Column, 0, len(cols)+1)
			result = append(result, cols[:i+1]...)
			result = append(result, fk)
			result = append(result, cols[i+1:]...)
			return result
		}
	}
	// No PK found — prepend
	return append([]*Column{fk}, cols...)
}
