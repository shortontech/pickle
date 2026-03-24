package migrations

type CreateJobsTable_2026_01_01_000002 struct {
	Migration
}

func (m *CreateJobsTable_2026_01_01_000002) Up() {
	m.CreateTable("jobs", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("user_id").NotNull().ForeignKey("users", "id")
		t.String("type", 100).NotNull()
		t.String("status", 50).NotNull().Default("pending")
		t.JSONB("payload").Nullable()
		t.Text("error").Nullable()
		t.Integer("attempts").NotNull().Default("0")
		t.Timestamps()
	})

	m.AddIndex("jobs", "status")
	m.AddIndex("jobs", "user_id")
}

func (m *CreateJobsTable_2026_01_01_000002) Down() {
	m.DropTableIfExists("jobs")
}
