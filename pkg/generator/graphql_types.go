package generator

import (
	"fmt"

	"github.com/shortontech/pickle/pkg/schema"
)

// graphqlType maps a Pickle column type to a GraphQL type string.
func graphqlType(col *schema.Column) string {
	switch col.Type {
	case schema.UUID:
		return "ID"
	case schema.String, schema.Text:
		return "String"
	case schema.Integer, schema.BigInteger:
		return "Int"
	case schema.Decimal:
		return "String" // precision-safe
	case schema.Boolean:
		return "Boolean"
	case schema.Timestamp:
		return "DateTime"
	case schema.Date:
		return "String"
	case schema.Time:
		return "String"
	case schema.JSONB:
		return "String"
	case schema.Binary:
		return "" // excluded
	default:
		return "String"
	}
}

// graphqlGoSerType returns the Go type used for serialization in GQL structs.
func graphqlGoSerType(col *schema.Column) string {
	switch col.Type {
	case schema.UUID:
		return "string"
	case schema.String, schema.Text:
		return "string"
	case schema.Integer:
		return "int"
	case schema.BigInteger:
		return "int64"
	case schema.Decimal:
		return "string"
	case schema.Boolean:
		return "bool"
	case schema.Timestamp, schema.Date, schema.Time:
		return "string"
	case schema.JSONB:
		return "string"
	default:
		return "string"
	}
}

// isExcludedFromGraphQL returns true if the column should not appear in the GraphQL schema.
func isExcludedFromGraphQL(col *schema.Column) bool {
	if col.Type == schema.Binary {
		return true
	}
	// Password fields are never exposed
	if col.Name == "password_hash" || col.Name == "password" {
		return true
	}
	// Hash chain internal columns
	if col.Name == "row_hash" || col.Name == "prev_hash" {
		return true
	}
	return false
}

// graphqlNullable returns "!" suffix if the column is not nullable, "" otherwise.
func graphqlNullable(col *schema.Column) string {
	if col.IsPrimaryKey || !col.IsNullable {
		return "!"
	}
	return ""
}

// isGraphQLExposed returns true if this table should appear in the GraphQL schema.
// Tables without a primary key are excluded.
func isGraphQLExposed(tbl *schema.Table) bool {
	return pkColumn(tbl) != nil
}

// hasVisibilityAnnotations returns true if any column has Public() or OwnerSees().
func hasVisibilityAnnotations(tbl *schema.Table) bool {
	for _, col := range tbl.Columns {
		if col.IsPublic || col.IsOwnerSees {
			return true
		}
	}
	return false
}

// hasWhereID returns true if the table has a primary key (and thus a WhereID method).
func hasWhereID(tbl *schema.Table) bool {
	return pkColumn(tbl) != nil
}

// pkColumn returns the primary key column for a table, or nil.
func pkColumn(tbl *schema.Table) *schema.Column {
	for _, col := range tbl.Columns {
		if col.IsPrimaryKey {
			return col
		}
	}
	return nil
}

// pkGoType returns the Go type of the primary key column.
func pkGoType(tbl *schema.Table) string {
	pk := pkColumn(tbl)
	if pk == nil {
		return "uuid.UUID"
	}
	return columnGoType(pk)
}

// pkIsUUID returns true if the primary key is a UUID type.
func pkIsUUID(tbl *schema.Table) bool {
	pk := pkColumn(tbl)
	return pk != nil && pk.Type == schema.UUID
}

// pkParseExpr returns Go code to parse a string ID into the PK type.
// Returns (parseExpr, needsError bool).
func pkParseExpr(tbl *schema.Table, varName string) (string, bool) {
	pk := pkColumn(tbl)
	if pk == nil || pk.Type == schema.UUID {
		return fmt.Sprintf("uuid.Parse(%s)", varName), true
	}
	// String PKs don't need parsing
	return varName, false
}
