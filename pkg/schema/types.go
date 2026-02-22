package schema

// ColumnType represents the data type of a database column.
type ColumnType int

const (
	UUID ColumnType = iota
	String
	Text
	Integer
	BigInteger
	Decimal
	Boolean
	Timestamp
	JSONB
	Date
	Time
	Binary
)
