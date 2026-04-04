//go:build ignore

package migrations

// CreateRoleUserTable_2026_03_23_000003 creates the role_user pivot table.
type CreateRoleUserTable_2026_03_23_000003 struct {
	Migration
}

func (m *CreateRoleUserTable_2026_03_23_000003) Up() {
	m.CreateTable("role_user", func(t *Table) {
		t.UUID("user_id").NotNull().ForeignKey("users", "id").OnDelete("CASCADE")
		t.UUID("role_id").NotNull().ForeignKey("roles", "id").OnDelete("CASCADE")
		t.Timestamps()
		t.PrimaryKey("user_id", "role_id")
	})
}

func (m *CreateRoleUserTable_2026_03_23_000003) Down() {
	m.DropTableIfExists("role_user")
}
