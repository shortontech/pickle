package tickler

import (
	"fmt"
	"os"
	"strings"

	"github.com/pickle-framework/pickle/pkg/names"
	"github.com/pickle-framework/pickle/pkg/schema"
)

// ScopeBlock represents a template block extracted from a scopes file.
type ScopeBlock struct {
	Scope string // "all", "string", "numeric", "timestamp"
	Body  string // The function template text
}

// ColumnDef holds the info needed to stamp out scope functions.
type ColumnDef struct {
	PascalName string // "UserID", "Status", "CreatedAt"
	SnakeName  string // "user_id", "status", "created_at"
	GoType     string // "uuid.UUID", "string", "time.Time"
	Scope      string // "all", "string", "numeric", "timestamp"
}

// scopeForType maps schema column types to their scope category.
func scopeForType(colType schema.ColumnType) string {
	switch colType {
	case schema.String, schema.Text:
		return "string"
	case schema.Integer, schema.BigInteger, schema.Decimal:
		return "numeric"
	case schema.Timestamp, schema.Date:
		return "timestamp"
	default:
		return "other"
	}
}

// ParseScopeBlocks reads a scopes template file and extracts the blocks.
func ParseScopeBlocks(path string) ([]ScopeBlock, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")
	var blocks []ScopeBlock
	var currentScope string
	var currentBody strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip build tag and package/import lines
		if strings.HasPrefix(trimmed, "//go:build") ||
			strings.HasPrefix(trimmed, "package ") ||
			strings.HasPrefix(trimmed, "import ") ||
			trimmed == "" && currentScope == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "// pickle:scope ") {
			// Save previous block if any
			if currentScope != "" {
				blocks = append(blocks, ScopeBlock{
					Scope: currentScope,
					Body:  strings.TrimSpace(currentBody.String()),
				})
			}
			currentScope = strings.TrimPrefix(trimmed, "// pickle:scope ")
			currentBody.Reset()
			continue
		}

		if trimmed == "// pickle:end" {
			if currentScope != "" {
				blocks = append(blocks, ScopeBlock{
					Scope: currentScope,
					Body:  strings.TrimSpace(currentBody.String()),
				})
			}
			currentScope = ""
			currentBody.Reset()
			continue
		}

		if currentScope != "" {
			currentBody.WriteString(line)
			currentBody.WriteString("\n")
		}
	}

	return blocks, nil
}

// ColumnsFromTable extracts column definitions from a schema table.
func ColumnsFromTable(table *schema.Table) []ColumnDef {
	var cols []ColumnDef
	for _, col := range table.Columns {
		cols = append(cols, ColumnDef{
			PascalName: names.SnakeToPascal(col.Name),
			SnakeName:  col.Name,
			GoType:     names.ColumnGoType(col),
			Scope:      scopeForType(col.Type),
		})
	}
	return cols
}

// GenerateScopes stamps out scope functions for each column of a table.
func GenerateScopes(blocks []ScopeBlock, columns []ColumnDef, modelName string) string {
	var b strings.Builder

	for _, col := range columns {
		for _, block := range blocks {
			if !scopeMatches(block.Scope, col.Scope) {
				continue
			}

			expanded := block.Body
			expanded = strings.ReplaceAll(expanded, "__Column__", col.PascalName)
			expanded = strings.ReplaceAll(expanded, "__column__", col.SnakeName)
			expanded = strings.ReplaceAll(expanded, "__type__", col.GoType)

			b.WriteString(expanded)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

// scopeMatches returns true if a block's scope applies to a column's scope.
func scopeMatches(blockScope, colScope string) bool {
	if blockScope == "all" {
		return true
	}
	return blockScope == colScope
}
