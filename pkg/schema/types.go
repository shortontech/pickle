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
	Float
	Double
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
	Float:      "float",
	Double:     "double",
}

func (t ColumnType) String() string {
	if int(t) < len(columnTypeNames) {
		if name := columnTypeNames[t]; name != "" {
			return name
		}
	}
	return "unknown"
}
