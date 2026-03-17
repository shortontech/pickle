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
	OpCreateView
	OpDropView
)

// Operation records a single schema change.
type Operation struct {
	Type         TableOperation
	Table        string
	TableDef     *Table
	ViewDef      *View
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
	if name == "" {
		panic("pickle: table name must not be empty")
	}
	t := &Table{Name: name}
	fn(t)
	m.Operations = append(m.Operations, Operation{
		Type:     OpCreateTable,
		Table:    name,
		TableDef: t,
	})
	// Flatten nested relationships into additional create table operations
	m.flattenRelationships(t)
}

// flattenRelationships walks a table's Relationships and emits OpCreateTable
// and OpAddIndex operations for each child table, with auto-injected FK columns.
func (m *Migration) flattenRelationships(parent *Table) {
	for _, rel := range parent.Relationships {
		child := rel.ChildTable
		// Inject FK column: {singular_parent}_id referencing parent PK
		parentSingular := singularize(parent.Name)
		pkType := findPKType(parent)
		fkCol := &Column{
			Name:             parentSingular + "_id",
			Type:             pkType,
			ForeignKeyTable:  parent.Name,
			ForeignKeyColumn: findPKName(parent),
			IsOwnerColumn:    true,
		}
		if rel.Type == RelHasOne {
			fkCol.IsUnique = true
		}
		child.Columns = insertFKColumn(child.Columns, fkCol)

		m.Operations = append(m.Operations, Operation{
			Type:     OpCreateTable,
			Table:    child.Name,
			TableDef: child,
		})
		// Add index on FK column
		m.Operations = append(m.Operations, Operation{
			Type:  OpAddIndex,
			Table: child.Name,
			Index: &Index{
				Table:   child.Name,
				Columns: []string{fkCol.Name},
			},
		})
		// Recurse for deeper nesting
		m.flattenRelationships(child)
	}
}

func (m *Migration) DropTableIfExists(name string) {
	m.Operations = append(m.Operations, Operation{
		Type:  OpDropTableIfExists,
		Table: name,
	})
}

func (m *Migration) RenameTable(oldName, newName string) {
	if oldName == "" || newName == "" {
		panic("pickle: RenameTable requires non-empty old and new names")
	}
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

// AlterTable allows adding columns and nested relationships to an existing table.
func (m *Migration) AlterTable(name string, fn func(*Table)) {
	t := &Table{Name: name}
	fn(t)
	// Emit AddColumn for each new column
	for _, col := range t.Columns {
		m.Operations = append(m.Operations, Operation{
			Type:  OpAddColumn,
			Table: name,
			ColumnDef: func(tmp *Table) {
				tmp.Columns = append(tmp.Columns, col)
			},
		})
	}
	// Flatten nested relationships
	m.flattenRelationships(t)
}

func (m *Migration) DropColumn(table, column string) {
	m.Operations = append(m.Operations, Operation{
		Type:       OpDropColumn,
		Table:      table,
		ColumnName: column,
	})
}

func (m *Migration) RenameColumn(table, oldName, newName string) {
	if table == "" || oldName == "" || newName == "" {
		panic("pickle: RenameColumn requires non-empty table, old, and new names")
	}
	m.Operations = append(m.Operations, Operation{
		Type:       OpRenameColumn,
		Table:      table,
		ColumnName: oldName,
		OldName:    oldName,
		NewName:    newName,
	})
}

func (m *Migration) AddIndex(table string, columns ...string) {
	if len(columns) == 0 {
		panic("pickle: AddIndex requires at least one column")
	}
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
	if len(columns) == 0 {
		panic("pickle: AddUniqueIndex requires at least one column")
	}
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

func (m *Migration) CreateView(name string, fn func(*View)) {
	v := &View{Name: name}
	fn(v)
	m.Operations = append(m.Operations, Operation{
		Type:    OpCreateView,
		Table:   name,
		ViewDef: v,
	})
}

func (m *Migration) DropView(name string) {
	m.Operations = append(m.Operations, Operation{
		Type:  OpDropView,
		Table: name,
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
