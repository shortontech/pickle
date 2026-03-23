package tickle

import (
	"fmt"
	"os"
	"strings"

	"github.com/shortontech/pickle/pkg/names"
	"github.com/shortontech/pickle/pkg/schema"
)

// ScopeBlock represents a template block extracted from a scopes file.
type ScopeBlock struct {
	Scope string // "all", "string", "numeric", "timestamp"
	Body  string // The function template text
}

// ColumnDef holds the info needed to stamp out scope functions.
type ColumnDef struct {
	PascalName  string // "UserID", "Status", "CreatedAt"
	SnakeName   string // "user_id", "status", "created_at"
	GoType      string // "uuid.UUID", "string", "time.Time"
	Scope       string // "all", "string", "numeric", "timestamp"
	IsEncrypted bool   // AES-SIV deterministic — only equality scopes
	IsSealed    bool   // AES-GCM non-deterministic — no scopes at all
}

// ScopeForType maps schema column types to their scope category.
func ScopeForType(colType schema.ColumnType) string {
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

	if currentScope != "" {
		return nil, fmt.Errorf("unclosed pickle:scope %q (missing pickle:end)", currentScope)
	}

	return blocks, nil
}

// ColumnsFromTable extracts column definitions from a schema table.
func ColumnsFromTable(table *schema.Table) []ColumnDef {
	var cols []ColumnDef
	for _, col := range table.Columns {
		cols = append(cols, ColumnDef{
			PascalName:  names.SnakeToPascal(col.Name),
			SnakeName:   col.Name,
			GoType:      names.ColumnGoType(col),
			Scope:       ScopeForType(col.Type),
			IsEncrypted: col.IsEncrypted,
			IsSealed:    col.IsSealed,
		})
	}
	return cols
}

// GenerateScopes stamps out scope functions for each column of a table.
// Returns an error if blocks are provided but none match any column.
func GenerateScopes(blocks []ScopeBlock, columns []ColumnDef, modelName string) (string, error) {
	if len(blocks) == 0 {
		return "", fmt.Errorf("no scope blocks provided for model %s", modelName)
	}

	var b strings.Builder
	matched := 0

	for _, col := range columns {
		// Sealed columns: skip ALL scopes (no search possible)
		if col.IsSealed {
			continue
		}

		for _, block := range blocks {
			if IsTableScope(block.Scope) {
				continue
			}
			if !scopeMatches(block.Scope, col.Scope) {
				continue
			}

			// Encrypted columns: only equality scopes (Where__Column__ and Where__Column__In)
			if col.IsEncrypted {
				body := block.Body
				isEquality := strings.Contains(body, "q.where(\"__column__\", val)") ||
					strings.Contains(body, "q.whereIn(\"__column__\", vals)")
				if !isEquality {
					continue
				}
			}

			expanded := block.Body
			expanded = strings.ReplaceAll(expanded, "QueryBuilder[T]", modelName+"Query")
			expanded = strings.ReplaceAll(expanded, "__Column__", col.PascalName)
			expanded = strings.ReplaceAll(expanded, "__column__", col.SnakeName)
			expanded = strings.ReplaceAll(expanded, "__type__", col.GoType)

			b.WriteString(expanded)
			b.WriteString("\n\n")
			matched++
		}
	}

	if matched == 0 {
		return "", fmt.Errorf("no scope blocks matched any column for model %s", modelName)
	}

	return b.String(), nil
}

// IsTableScope returns true if the scope is a table-level scope (not per-column).
func IsTableScope(scope string) bool {
	return scope == "table" || scope == "table_owned"
}

// GenerateTableScopes stamps out table-level scope functions for a model.
// "table" blocks are emitted for non-owned tables, "table_owned" for owned tables.
func GenerateTableScopes(blocks []ScopeBlock, modelName string, owned bool) string {
	var b strings.Builder

	wantScope := "table"
	if owned {
		wantScope = "table_owned"
	}

	for _, block := range blocks {
		if block.Scope != wantScope {
			continue
		}

		expanded := block.Body
		expanded = strings.ReplaceAll(expanded, "QueryBuilder[T]", modelName+"Query")
		expanded = strings.ReplaceAll(expanded, "__Model__", modelName)

		b.WriteString(expanded)
		b.WriteString("\n\n")
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
