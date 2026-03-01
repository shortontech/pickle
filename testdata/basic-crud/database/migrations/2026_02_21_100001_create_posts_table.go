package migrations

type CreatePostsTable_2026_02_21_100001 struct {
	Migration
}

func (m *CreatePostsTable_2026_02_21_100001) Up() {
	m.CreateTable("posts", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()").Public()
		t.UUID("user_id").NotNull().ForeignKey("users", "id").IsOwner()
		t.String("title", 255).NotNull().Public()
		t.Text("body").NotNull().OwnerSees()
		t.String("status").NotNull().Default("draft").Public()
		t.Timestamps()
	})

	m.AddIndex("posts", "user_id")
	m.AddIndex("posts", "status")
}

func (m *CreatePostsTable_2026_02_21_100001) Down() {
	m.DropTableIfExists("posts")
}
