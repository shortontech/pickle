package migrations

type CreateUsersTable_2026_02_21_100000 struct {
	Migration
}

func (m *CreateUsersTable_2026_02_21_100000) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name", 255).NotNull().Public()
		t.String("email", 255).NotNull().Unique().Public().UnsafePublic().Encrypted()
		t.String("password_hash", 255).NotNull().Encrypted()
		t.Timestamps()
	})

	m.AddIndex("users", "email")
}

func (m *CreateUsersTable_2026_02_21_100000) Down() {
	m.DropTableIfExists("users")
}
