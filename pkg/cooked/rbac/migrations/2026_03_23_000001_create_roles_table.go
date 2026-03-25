//go:build ignore

package migrations

// CreateRolesTable_2026_03_23_000001 creates the roles table for RBAC.
type CreateRolesTable_2026_03_23_000001 struct {
	Migration
}

func (m *CreateRolesTable_2026_03_23_000001) Up() {
	m.CreateTable("roles", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		t.String("slug", 50).NotNull().Unique()
		t.String("display_name", 100).NotNull()
		t.Boolean("is_manages").NotNull().Default("false")
		t.Boolean("is_default").NotNull().Default("false")
		t.String("birth_policy", 100).NotNull()
		t.Timestamps()
	})
}

func (m *CreateRolesTable_2026_03_23_000001) Down() {
	m.DropTableIfExists("roles")
}
