//go:build ignore

package migrations

// CreateRoleActionsTable_2026_03_23_000002 creates the role_actions table.
type CreateRoleActionsTable_2026_03_23_000002 struct {
	Migration
}

func (m *CreateRoleActionsTable_2026_03_23_000002) Up() {
	m.CreateTable("role_actions", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		t.String("role_slug", 50).NotNull().ForeignKey("roles", "slug")
		t.String("action", 100).NotNull()
		t.Timestamps()
	})

	m.AddIndex("role_actions", "role_slug")
	m.AddUniqueIndex("role_actions", "role_slug", "action")
}

func (m *CreateRoleActionsTable_2026_03_23_000002) Down() {
	m.DropTableIfExists("role_actions")
}
