//go:build ignore

package migration

import (
	"fmt"
	"strings"
)

type mysqlGenerator struct{}

func (g *mysqlGenerator) CreateTable(t *Table) string {
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
		defs = append(defs, g.columnDef(col, compositePK))
	}
	for _, col := range t.Columns {
		if col.IsPrimaryKey {
			primary = append(primary, mysqlQI(col.Name))
		}
	}
	if compositePK {
		defs = append(defs, "PRIMARY KEY ("+strings.Join(primary, ", ")+")")
	}
	for _, fk := range t.ForeignKeys {
		local := make([]string, len(fk.Columns))
		referenced := make([]string, len(fk.ReferencedColumns))
		for i := range fk.Columns {
			local[i] = mysqlQI(fk.Columns[i])
			referenced[i] = mysqlQI(fk.ReferencedColumns[i])
		}
		def := "FOREIGN KEY (" + strings.Join(local, ", ") + ") REFERENCES " + mysqlQI(fk.ReferencedTable) + " (" + strings.Join(referenced, ", ") + ")"
		if fk.OnDeleteAction != "" {
			def += " ON DELETE " + fk.OnDeleteAction
		}
		if fk.OnUpdateAction != "" {
			def += " ON UPDATE " + fk.OnUpdateAction
		}
		defs = append(defs, def)
	}
	return fmt.Sprintf("CREATE TABLE %s (\n\t%s\n)", mysqlQI(t.Name), strings.Join(defs, ",\n\t"))
}

func mysqlQI(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

func (g *mysqlGenerator) columnDef(col *Column, suppressInlinePK bool) string {
	var b strings.Builder
	b.WriteString(mysqlQI(col.Name))
	b.WriteByte(' ')
	switch col.Type {
	case UUID:
		b.WriteString("CHAR(36)")
	case String:
		if col.Length > 0 {
			fmt.Fprintf(&b, "VARCHAR(%d)", col.Length)
		} else {
			b.WriteString("VARCHAR(255)")
		}
	case Text:
		b.WriteString("TEXT")
	case Integer:
		b.WriteString("INT")
	case BigInteger:
		b.WriteString("BIGINT")
	case Decimal:
		fmt.Fprintf(&b, "DECIMAL(%d, %d)", col.Precision, col.Scale)
	case Boolean:
		b.WriteString("BOOLEAN")
	case Timestamp:
		b.WriteString("DATETIME")
	case JSONB:
		b.WriteString("JSON")
	case Date:
		b.WriteString("DATE")
	case Time:
		b.WriteString("TIME")
	case Binary:
		b.WriteString("BLOB")
	case Float:
		b.WriteString("FLOAT")
	case Double:
		b.WriteString("DOUBLE")
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
		b.WriteString(" REFERENCES " + mysqlQI(col.ForeignKeyTable) + "(" + mysqlQI(col.ForeignKeyColumn) + ")")
		if col.OnDeleteAction != "" {
			b.WriteString(" ON DELETE " + col.OnDeleteAction)
		}
	}
	return b.String()
}

func (g *mysqlGenerator) DropTableIfExists(name string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS `%s`", name)
}

func (g *mysqlGenerator) AddColumn(table string, col *Column) string {
	panic("mysql migration generator not yet implemented")
}

func (g *mysqlGenerator) DropColumn(table, column string) string {
	return fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table, column)
}

func (g *mysqlGenerator) RenameColumn(table, oldName, newName string) string {
	return fmt.Sprintf("ALTER TABLE `%s` RENAME COLUMN `%s` TO `%s`", table, oldName, newName)
}

func (g *mysqlGenerator) AddIndex(idx *Index) string {
	panic("mysql migration generator not yet implemented")
}

func (g *mysqlGenerator) RenameTable(oldName, newName string) string {
	return fmt.Sprintf("RENAME TABLE `%s` TO `%s`", oldName, newName)
}
