package names

import (
	"strings"
	"unicode"

	"github.com/shortontech/pickle/pkg/schema"
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

// SnakeToPascal converts snake_case to PascalCase, respecting common initialisms.
func SnakeToPascal(s string) string {
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

// PascalToSnake converts PascalCase to snake_case.
// UserController → user_controller, CreateUser → create_user, HTTPServer → http_server.
func PascalToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := rune(s[i-1])
				if unicode.IsLower(prev) {
					b.WriteRune('_')
				} else if unicode.IsUpper(prev) && i+1 < len(s) && unicode.IsLower(rune(s[i+1])) {
					b.WriteRune('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TableToStructName converts a table name to a singular PascalCase Go struct name.
func TableToStructName(name string) string {
	singular := strings.TrimSuffix(name, "s")
	return SnakeToPascal(singular)
}

// ColumnGoType returns the Go type string for a column.
func ColumnGoType(col *schema.Column) string {
	base := ColumnBaseGoType(col)
	if col.IsNullable && base != "[]byte" && base != "json.RawMessage" {
		return "*" + base
	}
	if col.IsNullable && base == "json.RawMessage" {
		return "*json.RawMessage"
	}
	return base
}

// ColumnBaseGoType returns the non-nullable Go type for a column.
func ColumnBaseGoType(col *schema.Column) string {
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

// ColumnImport returns the import path needed for a column's Go type.
func ColumnImport(col *schema.Column) string {
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
