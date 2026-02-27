//go:build ignore

package migration

import (
	"fmt"
	"strings"
)

type postgresGenerator struct{}

// qi quotes a Postgres identifier (table/column name).
func qi(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (g *postgresGenerator) CreateTable(t *Table) string {
	var cols []string
	for _, col := range t.Columns {
		cols = append(cols, g.columnDef(col))
	}
	return fmt.Sprintf("CREATE TABLE %s (\n\t%s\n)", qi(t.Name), strings.Join(cols, ",\n\t"))
}

func (g *postgresGenerator) columnDef(col *Column) string {
	var b strings.Builder
	b.WriteString(qi(col.Name))
	b.WriteString(" ")
	b.WriteString(g.columnType(col))

	if col.IsPrimaryKey {
		b.WriteString(" PRIMARY KEY")
	}
	if !col.IsNullable && !col.IsPrimaryKey {
		b.WriteString(" NOT NULL")
	}
	if col.IsUnique {
		b.WriteString(" UNIQUE")
	}
	if col.HasDefault {
		switch v := col.DefaultValue.(type) {
		case string:
			// Function calls (contain parens) pass through unquoted; string literals are quoted
			if strings.Contains(v, "(") {
				b.WriteString(fmt.Sprintf(" DEFAULT %s", v))
			} else {
				b.WriteString(fmt.Sprintf(" DEFAULT '%s'", v))
			}
		default:
			b.WriteString(fmt.Sprintf(" DEFAULT %v", v))
		}
	}
	if col.ForeignKeyTable != "" {
		b.WriteString(fmt.Sprintf(" REFERENCES %s(%s)", qi(col.ForeignKeyTable), qi(col.ForeignKeyColumn)))
	}
	return b.String()
}

func (g *postgresGenerator) columnType(col *Column) string {
	switch col.Type {
	case UUID:
		return "UUID"
	case String:
		if col.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", col.Length)
		}
		return "VARCHAR(255)"
	case Text:
		return "TEXT"
	case Integer:
		return "INTEGER"
	case BigInteger:
		return "BIGINT"
	case Decimal:
		if col.Precision > 0 {
			return fmt.Sprintf("NUMERIC(%d, %d)", col.Precision, col.Scale)
		}
		return "NUMERIC"
	case Boolean:
		return "BOOLEAN"
	case Timestamp:
		return "TIMESTAMPTZ"
	case JSONB:
		return "JSONB"
	case Date:
		return "DATE"
	case Time:
		return "TIME"
	case Binary:
		return "BYTEA"
	}
	return "TEXT"
}

func (g *postgresGenerator) DropTableIfExists(name string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", qi(name))
}

func (g *postgresGenerator) AddColumn(table string, col *Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", qi(table), g.columnDef(col))
}

func (g *postgresGenerator) DropColumn(table, column string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qi(table), qi(column))
}

func (g *postgresGenerator) RenameColumn(table, oldName, newName string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", qi(table), qi(oldName), qi(newName))
}

func (g *postgresGenerator) AddIndex(idx *Index) string {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	idxName := fmt.Sprintf("%s_%s_idx", idx.Table, strings.Join(idx.Columns, "_"))
	var quotedCols []string
	for _, c := range idx.Columns {
		quotedCols = append(quotedCols, qi(c))
	}
	return fmt.Sprintf(
		"CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, qi(idxName), qi(idx.Table), strings.Join(quotedCols, ", "),
	)
}

func (g *postgresGenerator) RenameTable(oldName, newName string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", qi(oldName), qi(newName))
}
