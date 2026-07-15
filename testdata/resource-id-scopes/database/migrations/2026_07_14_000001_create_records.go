package migrations

type CreateRecords_2026_07_14_000001 struct{ Migration }

func (m *CreateRecords_2026_07_14_000001) Up() {
	m.CreateTable("organizations", func(t *Table) {
		t.BigInteger("organization_id").PrimaryKey()
	})
	m.CreateTable("records", func(t *Table) {
		t.BigInteger("organization_id").NotNull()
		t.BigInteger("record_id").NotNull()
		t.String("name").NotNull()
		t.PrimaryKey("organization_id", "record_id")
		t.ForeignKey([]string{"organization_id"}, "organizations", []string{"organization_id"}).OnDelete("CASCADE")
	})
	m.CreateTable("record_notes", func(t *Table) {
		t.BigInteger("organization_id").NotNull()
		t.BigInteger("record_id").NotNull()
		t.BigInteger("note_id").NotNull()
		t.PrimaryKey("organization_id", "record_id", "note_id")
		t.ForeignKey(
			[]string{"organization_id", "record_id"},
			"records",
			[]string{"organization_id", "record_id"},
		).OnDelete("CASCADE")
	})
}

func (m *CreateRecords_2026_07_14_000001) Down() {
	m.DropTableIfExists("record_notes")
	m.DropTableIfExists("records")
	m.DropTableIfExists("organizations")
}
