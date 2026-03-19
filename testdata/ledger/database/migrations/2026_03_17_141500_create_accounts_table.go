package migrations

type CreateAccountsTable_2026_03_17_141500 struct {
	Migration
}

func (m *CreateAccountsTable_2026_03_17_141500) Up() {
	m.CreateTable("accounts", func(t *Table) {
		t.Immutable()

		t.UUID("owner_id").NotNull().ForeignKey("users", "id").IsOwner()
		t.String("name", 100).NotNull()
		t.String("currency", 3).NotNull()
		t.String("type", 50).NotNull() // checking, savings, credit
		t.Boolean("active").NotNull().Default(true)

		t.SoftDeletes()
	})
}

func (m *CreateAccountsTable_2026_03_17_141500) Down() {
	m.DropTableIfExists("accounts")
}
