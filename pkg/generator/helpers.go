package generator

import (
	"github.com/pickle-framework/pickle/pkg/names"
	"github.com/pickle-framework/pickle/pkg/schema"
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
