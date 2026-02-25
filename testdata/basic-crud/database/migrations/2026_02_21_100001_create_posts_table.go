package migrations

type CreatePostsTable_2026_02_21_100001 struct {
	Migration
}

func (m *CreatePostsTable_2026_02_21_100001) Up() {
	m.CreateTable("posts", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		t.UUID("user_id").NotNull().ForeignKey("users", "id")
		t.String("title", 255).NotNull()
		t.Text("body").NotNull()
		t.String("status").NotNull().Default("draft")
		t.Timestamps()
	})

	m.AddIndex("posts", "user_id")
	m.AddIndex("posts", "status")
}

func (m *CreatePostsTable_2026_02_21_100001) Down() {
	m.DropTableIfExists("posts")
}
