package migrations

type CreateUsersTable_2026_07_17_110000 struct{ Migration }

func (m *CreateUsersTable_2026_07_17_110000) Up() {
	m.CreateTable("users", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name", 255).NotNull()
		t.String("email", 255).NotNull().Unique()
		t.String("password_hash", 255).NotNull()
		t.String("role", 50).NotNull().Default("user")
		t.Timestamps()
	})

	m.AddIndex("users", "email")
	m.RawSQL(`INSERT INTO users (id, name, email, password_hash, role, created_at, updated_at)
VALUES (
    '018f0f4d-7b2a-7c26-8000-000000000001',
    'Demo Administrator',
    'admin@example.test',
    '$2a$10$gRGHNQkSWYpZMo2NPseTqOQMvKx4mXFJro7qvAXn6vqYvu34XssVK',
    'admin',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
) ON CONFLICT (email) DO NOTHING`)
}

func (m *CreateUsersTable_2026_07_17_110000) Down() {
	m.DropTableIfExists("users")
}
