package schema

// DriverCapabilities describes what a database driver supports.
// Used by the generator to make storage strategy decisions for
// nested schemas and query layer generation.
type DriverCapabilities struct {
	TransactionalDDL   bool // Can run DDL inside a transaction (Postgres: yes, MySQL: no)
	ConcurrentIndexing bool // Supports CREATE INDEX CONCURRENTLY (Postgres)
	JSONBSupport       bool // Native JSONB type (Postgres)
	UUIDNativeType     bool // Native UUID type (Postgres)
	AdvisoryLocks      bool // Advisory lock support (Postgres)
	ForeignKeys        bool // Foreign key constraints (SQL: yes, doc stores: no)
	EmbeddedDocs       bool // Embedded subdocuments (MongoDB, DynamoDB: yes)
	SecondaryIndex     bool // Secondary indexes
	UniqueIndex        bool // Unique index constraints
}

// DriverCaps returns capabilities for a given driver name.
func DriverCaps(driver string) DriverCapabilities {
	switch driver {
	case "pgsql", "postgres":
		return DriverCapabilities{
			TransactionalDDL:   true,
			ConcurrentIndexing: true,
			JSONBSupport:       true,
			UUIDNativeType:     true,
			AdvisoryLocks:      true,
			ForeignKeys:        true,
			SecondaryIndex:     true,
			UniqueIndex:        true,
		}
	case "mysql":
		return DriverCapabilities{
			ForeignKeys:    true,
			SecondaryIndex: true,
			UniqueIndex:    true,
		}
	case "sqlite":
		return DriverCapabilities{
			TransactionalDDL: true,
			ForeignKeys:      true,
			SecondaryIndex:   true,
			UniqueIndex:      true,
		}
	case "mongodb":
		return DriverCapabilities{
			EmbeddedDocs:   true,
			SecondaryIndex: true,
			UniqueIndex:    true,
		}
	case "dynamodb":
		return DriverCapabilities{
			EmbeddedDocs:   true,
			SecondaryIndex: true,
		}
	default:
		return DriverCapabilities{}
	}
}
