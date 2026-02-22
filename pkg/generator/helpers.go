package generator

import (
	"strings"

	"github.com/pickle-framework/pickle/pkg/schema"
)

// commonInitialisms are words that should be fully uppercased in Go names.
var commonInitialisms = map[string]string{
	"id":    "ID",
	"uuid":  "UUID",
	"url":   "URL",
	"uri":   "URI",
	"api":   "API",
	"http":  "HTTP",
	"https": "HTTPS",
	"ip":    "IP",
	"sql":   "SQL",
	"ssh":   "SSH",
	"json":  "JSON",
	"xml":   "XML",
	"html":  "HTML",
	"css":   "CSS",
	"cpu":   "CPU",
	"ram":   "RAM",
	"os":    "OS",
	"io":    "IO",
	"eof":   "EOF",
	"acl":   "ACL",
	"tls":   "TLS",
	"tcp":   "TCP",
	"udp":   "UDP",
	"dns":   "DNS",
}

// snakeToPascal converts snake_case to PascalCase, respecting common initialisms.
// Examples: "user_id" → "UserID", "created_at" → "CreatedAt", "brale_transfer_id" → "BraleTransferID"
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, part := range parts {
		if upper, ok := commonInitialisms[strings.ToLower(part)]; ok {
			b.WriteString(upper)
		} else {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// tableName converts a table name to a Go struct name (singular PascalCase).
// Examples: "users" → "User", "transfers" → "Transfer", "posts" → "Post"
func tableToStructName(name string) string {
	singular := strings.TrimSuffix(name, "s")
	return snakeToPascal(singular)
}

// columnGoType returns the Go type string for a column.
func columnGoType(col *schema.Column) string {
	base := columnBaseGoType(col)
	if col.IsNullable && base != "[]byte" && base != "json.RawMessage" {
		return "*" + base
	}
	if col.IsNullable && base == "json.RawMessage" {
		return "*json.RawMessage"
	}
	return base
}

func columnBaseGoType(col *schema.Column) string {
	switch col.Type {
	case schema.UUID:
		return "uuid.UUID"
	case schema.String, schema.Text, schema.Time:
		return "string"
	case schema.Integer:
		return "int"
	case schema.BigInteger:
		return "int64"
	case schema.Decimal:
		return "decimal.Decimal"
	case schema.Boolean:
		return "bool"
	case schema.Timestamp, schema.Date:
		return "time.Time"
	case schema.JSONB:
		return "json.RawMessage"
	case schema.Binary:
		return "[]byte"
	default:
		return "any"
	}
}

// columnImport returns the import path needed for a column's Go type, or empty string.
func columnImport(col *schema.Column) string {
	switch col.Type {
	case schema.UUID:
		return "github.com/google/uuid"
	case schema.Decimal:
		return "github.com/shopspring/decimal"
	case schema.Timestamp, schema.Date:
		return "time"
	case schema.JSONB:
		return "encoding/json"
	default:
		return ""
	}
}
