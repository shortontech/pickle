package migrations

type CreateUsersTable_2026_02_21_100000 struct {
	Migration
}

func (m *CreateUsersTable_2026_02_21_100000) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name", 255).NotNull()
		t.String("email", 255).NotNull().Unique()
		t.String("password", 255).NotNull()
		t.Timestamps()
	})

	m.AddIndex("users", "email")
}

func (m *CreateUsersTable_2026_02_21_100000) Down() {
	m.DropTableIfExists("users")
}
