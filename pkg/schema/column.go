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
