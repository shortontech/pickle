package migrations

type CreateTransfersTable_2026_03_17_100000 struct {
	Migration
}

func (m *CreateTransfersTable_2026_03_17_100000) Up() {
	m.CreateTable("transfers", func(t *Table) {
		t.Immutable()

		t.UUID("customer_id").NotNull().ForeignKey("customers", "id").IsOwner()
		t.String("status", 50).NotNull().Default("pending")
		t.Decimal("amount", 18, 2).NotNull()
		t.String("currency", 3).NotNull()

		t.SoftDeletes()
	})
}

func (m *CreateTransfersTable_2026_03_17_100000) Down() {
	m.DropTableIfExists("transfers")
}
