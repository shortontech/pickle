package schema

// Table collects column definitions for a database table.
type Table struct {
	Name    string
	Columns []*Column
}

func (t *Table) addColumn(name string, colType ColumnType) *Column {
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

// Timestamps adds created_at and updated_at columns.
func (t *Table) Timestamps() {
	t.addColumn("created_at", Timestamp).NotNull()
	t.addColumn("updated_at", Timestamp).NotNull()
}
