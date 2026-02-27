package schema

// Index represents a database index.
type Index struct {
	Table   string
	Columns []string
	Unique  bool
}

// TableOperation represents a schema change recorded by a migration.
type TableOperation int

const (
	OpCreateTable TableOperation = iota
	OpDropTableIfExists
	OpRenameTable
	OpAddColumn
	OpDropColumn
	OpRenameColumn
	OpAddIndex
	OpAddUniqueIndex
)

// Operation records a single schema change.
type Operation struct {
	Type         TableOperation
	Table        string
	TableDef     *Table
	Index        *Index
	NewName      string // for rename operations
	OldName      string // for rename operations
	ColumnName   string // for drop/rename column
	ColumnDef    func(*Table)
}

// Migration is the base type embedded by all migration structs.
// It records schema operations for later execution or inspection.
type Migration struct {
	Operations []Operation
}

func (m *Migration) CreateTable(name string, fn func(*Table)) {
	t := &Table{Name: name}
	fn(t)
	m.Operations = append(m.Operations, Operation{
		Type:     OpCreateTable,
		Table:    name,
		TableDef: t,
	})
}

func (m *Migration) DropTableIfExists(name string) {
	m.Operations = append(m.Operations, Operation{
		Type:  OpDropTableIfExists,
		Table: name,
	})
}

func (m *Migration) RenameTable(oldName, newName string) {
	m.Operations = append(m.Operations, Operation{
		Type:    OpRenameTable,
		Table:   oldName,
		OldName: oldName,
		NewName: newName,
	})
}

func (m *Migration) AddColumn(table string, fn func(*Table)) {
	m.Operations = append(m.Operations, Operation{
		Type:      OpAddColumn,
		Table:     table,
		ColumnDef: fn,
	})
}

func (m *Migration) DropColumn(table, column string) {
	m.Operations = append(m.Operations, Operation{
		Type:       OpDropColumn,
		Table:      table,
		ColumnName: column,
	})
}

func (m *Migration) RenameColumn(table, oldName, newName string) {
	m.Operations = append(m.Operations, Operation{
		Type:       OpRenameColumn,
		Table:      table,
		ColumnName: oldName,
		OldName:    oldName,
		NewName:    newName,
	})
}

func (m *Migration) AddIndex(table string, columns ...string) {
	m.Operations = append(m.Operations, Operation{
		Type:  OpAddIndex,
		Table: table,
		Index: &Index{
			Table:   table,
			Columns: columns,
			Unique:  false,
		},
	})
}

func (m *Migration) AddUniqueIndex(table string, columns ...string) {
	m.Operations = append(m.Operations, Operation{
		Type:  OpAddUniqueIndex,
		Table: table,
		Index: &Index{
			Table:   table,
			Columns: columns,
			Unique:  true,
		},
	})
}

// Reset clears recorded operations so the migration struct can be reused.
func (m *Migration) Reset() {
	m.Operations = nil
}

// GetOperations returns the operations recorded by Up() or Down().
func (m *Migration) GetOperations() []Operation {
	return m.Operations
}

// Transactional returns true — migrations run in a transaction by default.
// Override in concrete migration structs to opt out.
func (m *Migration) Transactional() bool {
	return true
}
