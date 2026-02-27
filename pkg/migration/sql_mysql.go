//go:build ignore

package migration

import "fmt"

type mysqlGenerator struct{}

func (g *mysqlGenerator) CreateTable(t *Table) string {
	panic("mysql migration generator not yet implemented")
}

func (g *mysqlGenerator) DropTableIfExists(name string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", name)
}

func (g *mysqlGenerator) AddColumn(table string, col *Column) string {
	panic("mysql migration generator not yet implemented")
}

func (g *mysqlGenerator) DropColumn(table, column string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, column)
}

func (g *mysqlGenerator) RenameColumn(table, oldName, newName string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, oldName, newName)
}

func (g *mysqlGenerator) AddIndex(idx *Index) string {
	panic("mysql migration generator not yet implemented")
}

func (g *mysqlGenerator) RenameTable(oldName, newName string) string {
	return fmt.Sprintf("RENAME TABLE %s TO %s", oldName, newName)
}
