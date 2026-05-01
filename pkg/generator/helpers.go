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

func safeParamName(s string) string {
	if goKeywords[s] {
		return s + "Value"
	}
	return s
}

var goKeywords = map[string]bool{
	"break":       true,
	"default":     true,
	"func":        true,
	"interface":   true,
	"select":      true,
	"case":        true,
	"defer":       true,
	"go":          true,
	"map":         true,
	"struct":      true,
	"chan":        true,
	"else":        true,
	"goto":        true,
	"package":     true,
	"switch":      true,
	"const":       true,
	"fallthrough": true,
	"if":          true,
	"range":       true,
	"type":        true,
	"continue":    true,
	"for":         true,
	"import":      true,
	"return":      true,
	"var":         true,
}
