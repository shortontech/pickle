package schema

// View represents a database view definition.
type View struct {
	Name         string
	Sources      []ViewSource
	Columns      []*ViewColumn
	GroupByCols  []string
}

// ViewSource represents a table referenced by a view (FROM or JOIN).
type ViewSource struct {
	Table         string
	Alias         string
	JoinType      string // "" for FROM, "JOIN", "LEFT JOIN"
	JoinCondition string
}

// ViewColumn represents a column in a view's SELECT list.
type ViewColumn struct {
	Column                       // embedded — carries Name, Type, Precision, Scale, IsNullable
	SourceAlias  string          // e.g. "t" from "t.id"
	SourceColumn string          // e.g. "id" from "t.id"
	OutputAlias  string          // optional alias for the output column name
	RawExpr      string          // raw SQL expression (for SelectRaw)
}

// OutputName returns the column name as it appears in the view's output.
func (vc *ViewColumn) OutputName() string {
	if vc.OutputAlias != "" {
		return vc.OutputAlias
	}
	if vc.SourceColumn != "" {
		return vc.SourceColumn
	}
	return vc.Name
}

// From sets the primary source table for the view.
func (v *View) From(table, alias string) {
	v.Sources = append(v.Sources, ViewSource{
		Table: table,
		Alias: alias,
	})
}

// Join adds an INNER JOIN to the view.
func (v *View) Join(table, alias, on string) {
	v.Sources = append(v.Sources, ViewSource{
		Table:         table,
		Alias:         alias,
		JoinType:      "JOIN",
		JoinCondition: on,
	})
}

// LeftJoin adds a LEFT JOIN to the view.
func (v *View) LeftJoin(table, alias, on string) {
	v.Sources = append(v.Sources, ViewSource{
		Table:         table,
		Alias:         alias,
		JoinType:      "LEFT JOIN",
		JoinCondition: on,
	})
}

// Column adds a column reference from a source table.
// ref is "alias.column" (e.g. "t.id"). An optional second argument aliases the output.
func (v *View) Column(ref string, alias ...string) {
	srcAlias, srcCol := parseColumnRef(ref)
	vc := &ViewColumn{
		SourceAlias:  srcAlias,
		SourceColumn: srcCol,
	}
	// Default the Name to the source column; overridden by alias if provided
	vc.Name = srcCol
	if len(alias) > 0 {
		vc.OutputAlias = alias[0]
		vc.Name = alias[0]
	}
	v.Columns = append(v.Columns, vc)
}

// SelectRaw adds a computed column with a raw SQL expression.
// Returns the ViewColumn so the caller can chain type builder methods.
func (v *View) SelectRaw(name, expr string) *ViewColumn {
	vc := &ViewColumn{
		RawExpr: expr,
	}
	vc.Name = name
	v.Columns = append(v.Columns, vc)
	return vc
}

// GroupBy sets the GROUP BY columns.
func (v *View) GroupBy(columns ...string) {
	v.GroupByCols = columns
}

// --- Type builder methods on ViewColumn (for SelectRaw) ---

func (vc *ViewColumn) BigInteger() *ViewColumn {
	vc.Type = BigInteger
	return vc
}

func (vc *ViewColumn) IntegerType() *ViewColumn {
	vc.Type = Integer
	return vc
}

func (vc *ViewColumn) Decimal(precision, scale int) *ViewColumn {
	vc.Type = Decimal
	vc.Precision = precision
	vc.Scale = scale
	return vc
}

func (vc *ViewColumn) StringType(length ...int) *ViewColumn {
	vc.Type = String
	if len(length) > 0 {
		vc.Length = length[0]
	} else {
		vc.Length = 255
	}
	return vc
}

func (vc *ViewColumn) TextType() *ViewColumn {
	vc.Type = Text
	return vc
}

func (vc *ViewColumn) BooleanType() *ViewColumn {
	vc.Type = Boolean
	return vc
}

func (vc *ViewColumn) TimestampType() *ViewColumn {
	vc.Type = Timestamp
	return vc
}

func (vc *ViewColumn) UUIDType() *ViewColumn {
	vc.Type = UUID
	return vc
}

func (vc *ViewColumn) JSONBType() *ViewColumn {
	vc.Type = JSONB
	return vc
}

// parseColumnRef splits "alias.column" into parts.
func parseColumnRef(ref string) (alias, column string) {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '.' {
			return ref[:i], ref[i+1:]
		}
	}
	return "", ref
}
