//go:build ignore

package migrations

// CreateGraphqlExposuresTable_2026_03_25_000002 creates the graphql_exposures table.
type CreateGraphqlExposuresTable_2026_03_25_000002 struct {
	Migration
}

func (m *CreateGraphqlExposuresTable_2026_03_25_000002) Up() {
	m.CreateTable("graphql_exposures", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		t.String("model", 100).NotNull()
		t.String("operation", 20).NotNull()
		t.Timestamps()
	})

	m.AddUniqueIndex("graphql_exposures", "model", "operation")
}

func (m *CreateGraphqlExposuresTable_2026_03_25_000002) Down() {
	m.DropTableIfExists("graphql_exposures")
}
