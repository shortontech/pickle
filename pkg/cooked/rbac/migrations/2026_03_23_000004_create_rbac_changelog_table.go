//go:build ignore

package migrations

// CreateRbacChangelogTable_2026_03_23_000004 creates the rbac_changelog table.
type CreateRbacChangelogTable_2026_03_23_000004 struct {
	Migration
}

func (m *CreateRbacChangelogTable_2026_03_23_000004) Up() {
	m.CreateTable("rbac_changelog", func(t *Table) {
		t.String("id", 255).PrimaryKey()
		t.Integer("batch").NotNull()
		t.String("state", 20).NotNull()
		t.Text("error").Nullable()
		t.Timestamp("started_at").Nullable()
		t.Timestamp("completed_at").Nullable()
	})
}

func (m *CreateRbacChangelogTable_2026_03_23_000004) Down() {
	m.DropTableIfExists("rbac_changelog")
}
