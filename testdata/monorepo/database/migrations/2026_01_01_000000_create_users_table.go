package migrations

type CreateUsersTable_2026_01_01_000000 struct {
	Migration
}

func (m *CreateUsersTable_2026_01_01_000000) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name", 255).NotNull()
		t.String("email", 255).NotNull().Unique()
		t.String("password", 255).NotNull()
		t.Timestamps()
	})
}

func (m *CreateUsersTable_2026_01_01_000000) Down() {
	m.DropTableIfExists("users")
}
