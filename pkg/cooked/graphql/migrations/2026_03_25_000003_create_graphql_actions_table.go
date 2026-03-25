//go:build ignore

package migrations

// CreateGraphqlActionsTable_2026_03_25_000003 creates the graphql_actions table.
type CreateGraphqlActionsTable_2026_03_25_000003 struct {
	Migration
}

func (m *CreateGraphqlActionsTable_2026_03_25_000003) Up() {
	m.CreateTable("graphql_actions", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		t.String("name", 100).NotNull().Unique()
		t.Timestamps()
	})
}

func (m *CreateGraphqlActionsTable_2026_03_25_000003) Down() {
	m.DropTableIfExists("graphql_actions")
}
