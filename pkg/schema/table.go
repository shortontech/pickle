package schema

// RelationshipType distinguishes HasMany from HasOne.
type RelationshipType int

const (
	RelHasMany RelationshipType = iota
	RelHasOne
)

// Relationship records a nested child table declared via HasMany/HasOne.
type Relationship struct {
	Type         RelationshipType
	Name         string // child table name (e.g., "posts")
	ChildTable   *Table // child schema definition
	ParentTable  string // parent table name (set during CreateTable)
	IsCollection bool   // .Collection() — separate collection for doc stores
	IsTopLevel   bool   // .TopLevelModel() — generate at models/ not models/parent/
}

func (r *Relationship) Collection() *Relationship {
	r.IsCollection = true
	return r
}

func (r *Relationship) TopLevelModel() *Relationship {
	r.IsTopLevel = true
	return r
}

// Table collects column definitions for a database table.
type Table struct {
	Name          string
	Connection    string // database connection name ("" = default)
	Columns       []*Column
	Relationships []*Relationship
	IsImmutable   bool // set by Immutable() — append-only, (id, version_id) composite PK
	HasSoftDelete bool // set by SoftDeletes() — adds deleted_at nullable column
}

func (t *Table) addColumn(name string, colType ColumnType) *Column {
	if name == "" {
		panic("pickle: column name must not be empty")
	}
	c := &Column{
		Name: name,
		Type: colType,
	}
	t.Columns = append(t.Columns, c)
	return c
}

func (t *Table) UUID(name string) *Column {
	return t.addColumn(name, UUID)
}

func (t *Table) String(name string, length ...int) *Column {
	c := t.addColumn(name, String)
	if len(length) > 0 {
		if length[0] < 1 {
			panic("pickle: string length must be >= 1")
		}
		c.Length = length[0]
	} else {
		c.Length = 255
	}
	return c
}

func (t *Table) Text(name string) *Column {
	return t.addColumn(name, Text)
}

func (t *Table) Integer(name string) *Column {
	return t.addColumn(name, Integer)
}

func (t *Table) BigInteger(name string) *Column {
	return t.addColumn(name, BigInteger)
}

func (t *Table) Decimal(name string, precision, scale int) *Column {
	if precision < 1 {
		panic("pickle: decimal precision must be >= 1")
	}
	if scale < 0 {
		panic("pickle: decimal scale must be >= 0")
	}
	if scale > precision {
		panic("pickle: decimal scale must not exceed precision")
	}
	c := t.addColumn(name, Decimal)
	c.Precision = precision
	c.Scale = scale
	return c
}

func (t *Table) Boolean(name string) *Column {
	return t.addColumn(name, Boolean)
}

func (t *Table) Timestamp(name string) *Column {
	return t.addColumn(name, Timestamp)
}

func (t *Table) JSONB(name string) *Column {
	return t.addColumn(name, JSONB)
}

func (t *Table) Date(name string) *Column {
	return t.addColumn(name, Date)
}

func (t *Table) Time(name string) *Column {
	return t.addColumn(name, Time)
}

func (t *Table) Binary(name string) *Column {
	return t.addColumn(name, Binary)
}

// HasMany declares a one-to-many relationship. The child table gets an
// auto-injected FK column pointing back to this table's primary key.
func (t *Table) HasMany(name string, fn func(*Table)) *Relationship {
	child := &Table{Name: name}
	fn(child)
	r := &Relationship{
		Type:        RelHasMany,
		Name:        name,
		ChildTable:  child,
		ParentTable: t.Name,
	}
	t.Relationships = append(t.Relationships, r)
	return r
}

// HasOne declares a one-to-one relationship. The child table gets an
// auto-injected unique FK column pointing back to this table's primary key.
func (t *Table) HasOne(name string, fn func(*Table)) *Relationship {
	child := &Table{Name: name}
	fn(child)
	r := &Relationship{
		Type:        RelHasOne,
		Name:        name,
		ChildTable:  child,
		ParentTable: t.Name,
	}
	t.Relationships = append(t.Relationships, r)
	return r
}

// Timestamps adds created_at and updated_at columns with NOW() defaults.
// Panics if called on an immutable table — use Immutable() instead,
// which derives created_at and updated_at from UUID v7 timestamps.
func (t *Table) Timestamps() {
	if t.IsImmutable {
		panic("pickle: Timestamps() must not be called on immutable table \"" + t.Name + "\" — CreatedAt and UpdatedAt are derived from UUID v7 timestamps in id and version_id")
	}
	t.addColumn("created_at", Timestamp).NotNull().Default("NOW()")
	t.addColumn("updated_at", Timestamp).NotNull().Default("NOW()")
}

// Immutable marks this table as append-only. Pickle injects id and version_id
// as a composite primary key and generates insert-on-update query behaviour.
// Do not call Timestamps() on an immutable table — created_at and updated_at
// are derived from the UUID v7 timestamps embedded in id and version_id.
func (t *Table) Immutable() {
	t.IsImmutable = true
	id := &Column{
		Name:         "id",
		Type:         UUID,
		IsPrimaryKey: true,
	}
	versionID := &Column{
		Name:         "version_id",
		Type:         UUID,
		IsPrimaryKey: true,
	}
	t.Columns = append([]*Column{id, versionID}, t.Columns...)
}

// SoftDeletes adds a nullable deleted_at timestamp column.
// On an immutable table, Delete() inserts a new version with deleted_at set.
// On a mutable table, Delete() issues a standard soft-delete UPDATE.
func (t *Table) SoftDeletes() {
	t.HasSoftDelete = true
	t.addColumn("deleted_at", Timestamp).Nullable()
}
