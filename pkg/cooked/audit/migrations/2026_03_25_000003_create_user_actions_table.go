//go:build ignore

package migrations

// CreateUserActionsTable_2026_03_25_000003 creates the user_actions table for audit trails.
type CreateUserActionsTable_2026_03_25_000003 struct {
	Migration
}

func (m *CreateUserActionsTable_2026_03_25_000003) Up() {
	m.CreateTable("user_actions", func(t *Table) {
		t.AppendOnly()
		t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		t.UUID("user_id").NotNull().ForeignKey("users", "id")
		t.Integer("action_type_id").NotNull().ForeignKey("action_types", "id")
		t.String("resource_id", 255).NotNull()
		t.UUID("resource_version_id").Nullable()
		t.UUID("role_id").Nullable().ForeignKey("roles", "id")
		t.String("ip_address", 45).NotNull()
		t.String("request_id", 255).NotNull()
		t.Timestamp("created_at").NotNull().Default("NOW()")
	})

	m.AddIndex("user_actions", "user_id")
	m.AddIndex("user_actions", "action_type_id")
	m.AddIndex("user_actions", "resource_id")
	m.AddIndex("user_actions", "request_id")
}

func (m *CreateUserActionsTable_2026_03_25_000003) Down() {
	m.DropTableIfExists("user_actions")
}
