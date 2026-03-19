package migrations

type CreateTransactionsTable_2026_03_17_141501 struct {
	Migration
}

func (m *CreateTransactionsTable_2026_03_17_141501) Up() {
	m.CreateTable("transactions", func(t *Table) {
		t.AppendOnly()

		t.UUID("account_id").NotNull().ForeignKey("accounts", "id").IsOwner()
		t.String("type", 50).NotNull()   // debit, credit, fee, reversal
		t.Decimal("amount", 18, 8).NotNull()
		t.String("currency", 3).NotNull()
		t.String("description", 255).Nullable()
		t.UUID("reverses_id").Nullable() // points to the transaction being reversed
	})

	m.AddIndex("transactions", "account_id")
	m.AddIndex("transactions", "reverses_id")
}

func (m *CreateTransactionsTable_2026_03_17_141501) Down() {
	m.DropTableIfExists("transactions")
}
