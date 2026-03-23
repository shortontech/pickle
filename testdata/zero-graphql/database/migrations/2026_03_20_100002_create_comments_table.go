package migrations

type CreateCommentsTable_2026_03_20_100002 struct {
	Migration
}

func (m *CreateCommentsTable_2026_03_20_100002) Up() {
	m.CreateTable("comments", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("post_id").NotNull().ForeignKey("posts", "id")
		t.UUID("user_id").NotNull().ForeignKey("users", "id").IsOwner()
		t.Text("body").NotNull().Public()
		t.Timestamps()
	})

	m.AddIndex("comments", "post_id")
	m.AddIndex("comments", "user_id")
}

func (m *CreateCommentsTable_2026_03_20_100002) Down() {
	m.DropTableIfExists("comments")
}
