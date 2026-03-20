package migrations

type CreateUsersTable_2026_03_17_141425 struct {
	Migration
}

func (m *CreateUsersTable_2026_03_17_141425) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name").NotNull()
		t.String("email").NotNull().Unique()
		t.String("password_hash").NotNull()
		t.Timestamps()
	})
}

func (m *CreateUsersTable_2026_03_17_141425) Down() {
	m.DropTableIfExists("users")
}
