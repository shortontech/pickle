//go:build ignore

package migration

import "fmt"

type sqliteGenerator struct{}

func (g *sqliteGenerator) CreateTable(t *Table) string {
	panic("sqlite migration generator not yet implemented")
}

func (g *sqliteGenerator) DropTableIfExists(name string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", name)
}

func (g *sqliteGenerator) AddColumn(table string, col *Column) string {
	panic("sqlite migration generator not yet implemented")
}

func (g *sqliteGenerator) DropColumn(table, column string) string {
	panic("sqlite: DROP COLUMN requires recreating the table")
}

func (g *sqliteGenerator) RenameColumn(table, oldName, newName string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, oldName, newName)
}

func (g *sqliteGenerator) AddIndex(idx *Index) string {
	panic("sqlite migration generator not yet implemented")
}

func (g *sqliteGenerator) RenameTable(oldName, newName string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldName, newName)
}
