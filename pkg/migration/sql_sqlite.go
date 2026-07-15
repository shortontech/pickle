//go:build ignore

package migration

import (
	"fmt"
	"strings"
)

type sqliteGenerator struct{}

func (g *sqliteGenerator) CreateTable(t *Table) string {
	pkCount := 0
	for _, col := range t.Columns {
		if col.IsPrimaryKey {
			pkCount++
		}
	}
	compositePK := pkCount > 1
	defs := make([]string, 0, len(t.Columns)+len(t.ForeignKeys)+1)
	var primary []string
	for _, col := range expandColumns(t.Columns) {
		defs = append(defs, sqliteColumnDef(col, compositePK))
	}
	for _, col := range t.Columns {
		if col.IsPrimaryKey {
			primary = append(primary, sqliteQI(col.Name))
		}
	}
	if compositePK {
		defs = append(defs, "PRIMARY KEY ("+strings.Join(primary, ", ")+")")
	}
	for _, fk := range t.ForeignKeys {
		local := make([]string, len(fk.Columns))
		referenced := make([]string, len(fk.ReferencedColumns))
		for i := range fk.Columns {
			local[i] = sqliteQI(fk.Columns[i])
			referenced[i] = sqliteQI(fk.ReferencedColumns[i])
		}
		def := "FOREIGN KEY (" + strings.Join(local, ", ") + ") REFERENCES " + sqliteQI(fk.ReferencedTable) + " (" + strings.Join(referenced, ", ") + ")"
		if fk.OnDeleteAction != "" {
			def += " ON DELETE " + fk.OnDeleteAction
		}
		if fk.OnUpdateAction != "" {
			def += " ON UPDATE " + fk.OnUpdateAction
		}
		defs = append(defs, def)
	}
	return fmt.Sprintf("CREATE TABLE %s (\n\t%s\n)", sqliteQI(t.Name), strings.Join(defs, ",\n\t"))
}

func sqliteQI(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func sqliteColumnDef(col *Column, suppressInlinePK bool) string {
	var b strings.Builder
	b.WriteString(sqliteQI(col.Name))
	b.WriteByte(' ')
	switch col.Type {
	case Integer, BigInteger, Boolean:
		b.WriteString("INTEGER")
	case Decimal, Float, Double:
		b.WriteString("REAL")
	case Binary:
		b.WriteString("BLOB")
	default:
		b.WriteString("TEXT")
	}
	if col.IsPrimaryKey && !suppressInlinePK {
		b.WriteString(" PRIMARY KEY")
	}
	if !col.IsNullable && !(col.IsPrimaryKey && !suppressInlinePK) {
		b.WriteString(" NOT NULL")
	}
	if col.IsUnique {
		b.WriteString(" UNIQUE")
	}
	if col.ForeignKeyTable != "" && !col.FKMetadataOnly {
		b.WriteString(" REFERENCES " + sqliteQI(col.ForeignKeyTable) + "(" + sqliteQI(col.ForeignKeyColumn) + ")")
		if col.OnDeleteAction != "" {
			b.WriteString(" ON DELETE " + col.OnDeleteAction)
		}
	}
	return b.String()
}

func (g *sqliteGenerator) DropTableIfExists(name string) string {
	return fmt.Sprintf(`DROP TABLE IF EXISTS "%s"`, name)
}

func (g *sqliteGenerator) AddColumn(table string, col *Column) string {
	panic("sqlite migration generator not yet implemented")
}

func (g *sqliteGenerator) DropColumn(table, column string) string {
	panic("sqlite: DROP COLUMN requires recreating the table")
}

func (g *sqliteGenerator) RenameColumn(table, oldName, newName string) string {
	return fmt.Sprintf(`ALTER TABLE "%s" RENAME COLUMN "%s" TO "%s"`, table, oldName, newName)
}

func (g *sqliteGenerator) AddIndex(idx *Index) string {
	panic("sqlite migration generator not yet implemented")
}

func (g *sqliteGenerator) RenameTable(oldName, newName string) string {
	return fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, oldName, newName)
}
