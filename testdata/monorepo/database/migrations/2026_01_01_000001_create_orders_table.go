package migrations

type CreateOrdersTable_2026_01_01_000001 struct {
	Migration
}

func (m *CreateOrdersTable_2026_01_01_000001) Up() {
	m.CreateTable("orders", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("user_id").NotNull().ForeignKey("users", "id")
		t.String("status", 50).NotNull().Default("pending")
		t.Decimal("total", 12, 2).NotNull()
		t.String("currency", 3).NotNull().Default("USD")
		t.Timestamps()
	})

	m.AddIndex("orders", "user_id")
	m.AddIndex("orders", "status")
}

func (m *CreateOrdersTable_2026_01_01_000001) Down() {
	m.DropTableIfExists("orders")
}
