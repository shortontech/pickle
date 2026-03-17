package migrations

type CreateAccountsTable_2026_03_03_123231 struct {
	Migration
}

func (m *CreateAccountsTable_2026_03_03_123231) Up() {
	m.CreateTable("accounts", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("api_key", 255).NotNull().Encrypted()
		t.Timestamps()
	})
}

func (m *CreateAccountsTable_2026_03_03_123231) Down() {
	m.DropTableIfExists("accounts")
}
