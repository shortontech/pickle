package schema

// Column represents a database column definition.
type Column struct {
	Name             string
	Type             ColumnType
	Length           int
	Precision        int
	Scale            int
	IsPrimaryKey     bool
	IsNullable       bool
	IsUnique         bool
	DefaultValue     any
	HasDefault       bool
	ForeignKeyTable  string
	ForeignKeyColumn string
	IsPublic         bool
	IsOwnerSees      bool
	IsOwnerColumn    bool
	IsEncrypted      bool
	IsSealed         bool
	IsUnsafePublic   bool
	OnDeleteAction   string          // e.g. "CASCADE", "SET NULL" — appended to FK constraint
	FKMetadataOnly   bool // FK is for ORM relationship metadata only; no SQL REFERENCES constraint
	VisibleTo        map[string]bool   // role slugs that can see this column
	VisibleToSource  map[string]string // role slug → migration ID that added the annotation
}

func (c *Column) PrimaryKey() *Column {
	c.IsPrimaryKey = true
	return c
}

func (c *Column) NotNull() *Column {
	c.IsNullable = false
	return c
}

func (c *Column) Nullable() *Column {
	c.IsNullable = true
	return c
}

func (c *Column) Unique() *Column {
	c.IsUnique = true
	return c
}

func (c *Column) Default(value any) *Column {
	c.DefaultValue = value
	c.HasDefault = true
	return c
}

func (c *Column) ForeignKey(table, column string) *Column {
	c.ForeignKeyTable = table
	c.ForeignKeyColumn = column
	return c
}

// Public marks this column as visible to anyone (no auth required).
func (c *Column) Public() *Column {
	c.IsPublic = true
	return c
}

// OwnerSees marks this column as visible only to the row's owner.
func (c *Column) OwnerSees() *Column {
	c.IsOwnerSees = true
	return c
}

// Encrypted marks this column as requiring encryption at rest.
func (c *Column) Encrypted() *Column {
	c.IsEncrypted = true
	return c
}

// Sealed marks this column as requiring non-deterministic encryption at rest (AES-256-GCM).
// Sealed columns cannot be searched — no WHERE scopes are generated.
func (c *Column) Sealed() *Column {
	c.IsSealed = true
	return c
}

// UnsafePublic explicitly acknowledges that a sensitive-named column is intentionally
// marked Public. Without this, Squeeze flags the Public/sensitive combination as an error.
func (c *Column) UnsafePublic() *Column {
	c.IsUnsafePublic = true
	return c
}

// RoleSees marks this column as visible to the specified role slug.
func (c *Column) RoleSees(slug string) *Column {
	if c.VisibleTo == nil {
		c.VisibleTo = map[string]bool{}
	}
	c.VisibleTo[slug] = true
	return c
}

// RoleSeesFrom marks this column as visible to the specified role slug,
// recording the migration ID that introduced the annotation. Used for
// birth-timestamp filtering: annotations from before a role's birth are skipped.
func (c *Column) RoleSeesFrom(slug, migrationID string) *Column {
	c.RoleSees(slug)
	if c.VisibleToSource == nil {
		c.VisibleToSource = map[string]string{}
	}
	c.VisibleToSource[slug] = migrationID
	return c
}

// OnDelete sets the ON DELETE action for a foreign key column (e.g. "CASCADE", "SET NULL").
func (c *Column) OnDelete(action string) *Column {
	c.OnDeleteAction = action
	return c
}

// IsOwner marks this column as the ownership column for the table.
// The value of this column is compared against the authenticated user's ID
// to determine ownership. Only one column per table may be marked as owner.
func (c *Column) IsOwner() *Column {
	c.IsOwnerColumn = true
	return c
}
