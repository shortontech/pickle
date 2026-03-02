package generator

import (
	"unicode"
	"unicode/utf8"

	"github.com/shortontech/pickle/pkg/names"
	"github.com/shortontech/pickle/pkg/schema"
)

func snakeToPascal(s string) string {
	return names.SnakeToPascal(s)
}

func tableToStructName(name string) string {
	return names.TableToStructName(name)
}

func columnGoType(col *schema.Column) string {
	return names.ColumnGoType(col)
}

func columnImport(col *schema.Column) string {
	return names.ColumnImport(col)
}

func toLowerFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}
