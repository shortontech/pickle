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

var columnTypeNames = [...]string{
	UUID:       "uuid",
	String:     "string",
	Text:       "text",
	Integer:    "integer",
	BigInteger: "bigint",
	Decimal:    "decimal",
	Boolean:    "boolean",
	Timestamp:  "timestamp",
	JSONB:      "jsonb",
	Date:       "date",
	Time:       "time",
	Binary:     "binary",
}

func (t ColumnType) String() string {
	if int(t) < len(columnTypeNames) {
		return columnTypeNames[t]
	}
	return "unknown"
}
