//go:build ignore

package migrations

// CreateOauthTokensTable_2026_03_03_200000 creates the oauth_tokens table for OAuth2 client credentials tokens.
type CreateOauthTokensTable_2026_03_03_200000 struct {
	Migration
}

func (m *CreateOauthTokensTable_2026_03_03_200000) Up() {
	m.CreateTable("oauth_tokens", func(t *Table) {
		t.String("token", 255).PrimaryKey()
		t.String("client_id", 255).NotNull()
		t.Timestamp("expires_at").NotNull()
		t.Timestamp("created_at").NotNull().Default("NOW()")
	})

	m.AddIndex("oauth_tokens", "client_id")
}

func (m *CreateOauthTokensTable_2026_03_03_200000) Down() {
	m.DropTableIfExists("oauth_tokens")
}
