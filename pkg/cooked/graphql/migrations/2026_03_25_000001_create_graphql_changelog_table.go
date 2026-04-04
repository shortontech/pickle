//go:build ignore

package migrations

// CreateGraphqlChangelogTable_2026_03_25_000001 creates the graphql_changelog table.
type CreateGraphqlChangelogTable_2026_03_25_000001 struct {
	Migration
}

func (m *CreateGraphqlChangelogTable_2026_03_25_000001) Up() {
	m.CreateTable("graphql_changelog", func(t *Table) {
		t.String("id").PrimaryKey()
		t.Integer("batch").NotNull()
		t.String("state").NotNull()
		t.Text("error").Nullable()
		t.Timestamp("started_at").Nullable()
		t.Timestamp("completed_at").Nullable()
	})
}

func (m *CreateGraphqlChangelogTable_2026_03_25_000001) Down() {
	m.DropTableIfExists("graphql_changelog")
}
