//go:build ignore

package migrations

// CreateJwtTokensTable_2026_03_03_100000 creates the jwt_tokens table for token revocation.
type CreateJwtTokensTable_2026_03_03_100000 struct {
	Migration
}

func (m *CreateJwtTokensTable_2026_03_03_100000) Up() {
	m.CreateTable("jwt_tokens", func(t *Table) {
		t.String("jti", 255).PrimaryKey()
		t.UUID("user_id").NotNull()
		t.Timestamp("expires_at").NotNull()
		t.Timestamp("revoked_at").Nullable()
		t.Timestamp("created_at").NotNull().Default("NOW()")
	})

	m.AddIndex("jwt_tokens", "user_id")
}

func (m *CreateJwtTokensTable_2026_03_03_100000) Down() {
	m.DropTableIfExists("jwt_tokens")
}
