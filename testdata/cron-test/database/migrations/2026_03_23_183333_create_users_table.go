package migrations

type CreateUsersTable_2026_03_23_183333 struct {
	Migration
}

func (m *CreateUsersTable_2026_03_23_183333) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name").NotNull()
		t.String("email").NotNull().Unique()
		t.String("password").NotNull()
		t.Timestamps()
	})
}

func (m *CreateUsersTable_2026_03_23_183333) Down() {
	m.DropTableIfExists("users")
}
