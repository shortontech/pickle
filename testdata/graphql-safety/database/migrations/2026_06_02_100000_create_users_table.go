package migrations

type CreateUsersTable_2026_06_02_100000 struct {
	Migration
}

func (m *CreateUsersTable_2026_06_02_100000) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name", 255).NotNull().Public()
		t.String("email", 255).NotNull().OwnerSees()
		t.String("password_hash", 255).NotNull()
		t.Timestamps()
	})

	m.CreateTable("posts", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("user_id").NotNull().ForeignKey("users", "id").IsOwner()
		t.String("title", 255).NotNull().Public()
		t.Text("body").NotNull()
		t.Timestamps()
	})

	m.CreateTable("comments", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("post_id").NotNull().ForeignKey("posts", "id")
		t.UUID("user_id").NotNull().ForeignKey("users", "id").IsOwner()
		t.Text("body").NotNull().Public()
		t.Timestamps()
	})
}

func (m *CreateUsersTable_2026_06_02_100000) Down() {
	m.DropTableIfExists("comments")
	m.DropTableIfExists("posts")
	m.DropTableIfExists("users")
}
