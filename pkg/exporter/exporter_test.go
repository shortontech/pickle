package exporter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

func TestExportBasicCRUDNoPickleImports(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	writeTestAction(t, projectDir)
	out := filepath.Join(t.TempDir(), "exported")
	res, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if res.FilesWritten == 0 {
		t.Fatal("expected exported files")
	}
	if hasFinding(res.Findings, "generated_auth") {
		t.Fatalf("did not expect generated_auth finding, got %+v", res.Findings)
	}
	if !hasFinding(res.Findings, "rbac_policy_export") {
		t.Fatalf("expected rbac_policy_export finding, got %+v", res.Findings)
	}
	if !hasFinding(res.Findings, "generated_graphql_policies") {
		t.Fatalf("expected generated_graphql_policies finding, got %+v", res.Findings)
	}
	if !hasFinding(res.Findings, "actions_audit") {
		t.Fatalf("expected actions_audit finding, got %+v", res.Findings)
	}
	assertFileContains(t, filepath.Join(out, "go.mod"), "gorm.io/gorm")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "type User struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_post_stat.go"), "type UserPostStat struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "db.go"), "var DB *gorm.DB")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "CREATE TABLE")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.down.sql"), "DROP TABLE")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "CREATE INDEX")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260228100000_create_user_post_stats_view.up.sql"), "CREATE VIEW")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Exported")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Partial Support")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone JWT, OAuth client-credentials, and session auth drivers")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "rbac_policy_export")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "generated_graphql_policies")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Omitted")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func Env(key, fallback string) string")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "type ConnectionConfig struct")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func OpenGORM(conn ConnectionConfig) *gorm.DB")
	assertFileContains(t, filepath.Join(out, "config", "app.go"), "func app() AppConfig")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func BuiltinCommands() []Command")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "support.go"), "func (r *Runner) Migrate(entries []MigrationEntry) error")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "crypto/hmac")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "ErrInvalidToken")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "oauth.NewDriver")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "session.NewDriver")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "func DefaultAuthMiddleware")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "func ActiveDriverName")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "func (d *Driver) TokenEndpoint")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func CSRF")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_ban.go"), "DB.Save(user).Error")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_promote.go"), "type PromoteResult struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_standalone_gate.go"), "func CanView")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_ban_gate_gen.go"), `HasAnyRole("admin")`)
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func (m *User) Ban")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func (m *User) Promote")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "CanBan(ctx, m)")
	assertFileContains(t, filepath.Join(out, "app", "services", "action_call.go"), "models.BanAction")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "models.DB.Model(&models.User{})")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "QueryUser")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "basic-crud/internal/httpx")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Target ORM: `gorm`")

	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	assertNoGoFileContains(t, out, "PICKLE_")
	assertFileContains(t, filepath.Join(out, "go.sum"), "gorm.io/gorm")
	writeExportedAuthBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedAuthBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package auth_test

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"basic-crud/app/http/auth"
	"basic-crud/app/http/auth/jwt"
	"basic-crud/app/http/auth/oauth"
	"basic-crud/app/http/auth/session"
)

func TestExportedAuthDriversPreserveBehavior(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE oauth_tokens (token TEXT PRIMARY KEY, client_id TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	env := func(key, fallback string) string {
		values := map[string]string{
			"DB_CONNECTION": "sqlite",
			"JWT_SECRET": "0123456789abcdef0123456789abcdef",
			"OAUTH_CLIENT_ID": "client-1",
			"OAUTH_CLIENT_SECRET": "secret-1",
			"SESSION_SECRET": "session-secret",
		}
		if value, ok := values[key]; ok {
			return value
		}
		return fallback
	}
	auth.Init(env, db)

	jwtDriver := auth.Driver("jwt").(*jwt.Driver)
	token, err := jwtDriver.SignToken(jwt.Claims{Subject: "user-1", Role: "admin"})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	info, err := jwtDriver.Authenticate(req)
	if err != nil {
		t.Fatalf("authenticate jwt: %v", err)
	}
	if info.UserID != "user-1" || info.Role != "admin" {
		t.Fatalf("jwt auth info = %#v", info)
	}
	if _, err := db.Exec("DELETE FROM jwt_tokens"); err != nil {
		t.Fatal(err)
	}
	if _, err := jwtDriver.Authenticate(req); err == nil {
		t.Fatal("revoked jwt should fail allowlist validation")
	}

	oauthDriver := auth.Driver("oauth").(*oauth.Driver)
	if _, err := db.Exec("INSERT INTO oauth_tokens (token, client_id, expires_at, created_at) VALUES (?, ?, ?, ?)", "opaque", "client-1", time.Now().Add(time.Hour), time.Now()); err != nil {
		t.Fatal(err)
	}
	oauthInfo, err := oauthDriver.ValidateToken("opaque")
	if err != nil {
		t.Fatalf("validate oauth token: %v", err)
	}
	if oauthInfo.UserID != "client-1" || oauthInfo.Role != "client" {
		t.Fatalf("oauth auth info = %#v", oauthInfo)
	}

	sessionDriver := auth.Driver("session").(*session.Driver)
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", "sess-1", "user-2", "viewer", time.Now().Add(time.Hour), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	sessionReq, _ := http.NewRequest("GET", "/", nil)
	sessionReq.AddCookie(&http.Cookie{Name: sessionDriver.CookieName(), Value: "sess-1"})
	sessionInfo, err := sessionDriver.Authenticate(sessionReq)
	if err != nil {
		t.Fatalf("authenticate session: %v", err)
	}
	if sessionInfo.UserID != "user-2" || sessionInfo.Role != "viewer" {
		t.Fatalf("session auth info = %#v", sessionInfo)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "http", "auth", "exported_auth_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestAction(t *testing.T, projectDir string) {
	t.Helper()
	dir := filepath.Join(projectDir, "database", "actions", "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	action := `package user

import (
	models "github.com/shortontech/pickle/testdata/basic-crud/app/models"
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type BanAction struct { Reason string }

func (a BanAction) Ban(ctx *pickle.Context, user *models.User) error {
	user.Name = a.Reason
	return models.QueryUser().Update(user)
}
`
	promote := `package user

import (
	models "github.com/shortontech/pickle/testdata/basic-crud/app/models"
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type PromoteAction struct { Level string }
type PromoteResult struct { Level string }

func (a PromoteAction) Promote(ctx *pickle.Context, user *models.User) (*PromoteResult, error) {
	return &PromoteResult{Level: a.Level}, nil
}
`
	standaloneGate := `package user

import "github.com/google/uuid"

func CanView(ctx *Context, user *User) *uuid.UUID {
	if ctx.IsAuthenticated() {
		id := uuid.New()
		return &id
	}
	return nil
}
`
	policy := `package policies

type GrantBan_2026_03_24_100000 struct { Policy }

func (m *GrantBan_2026_03_24_100000) Up() { m.AlterRole("admin").Can("Ban", "Promote") }
func (m *GrantBan_2026_03_24_100000) Down() { m.AlterRole("admin").RevokeCan("Ban", "Promote") }
`
	callSite := `package services

import (
	models "github.com/shortontech/pickle/testdata/basic-crud/app/models"
	useractions "github.com/shortontech/pickle/testdata/basic-crud/database/actions/user"
)

func NewBanAction() useractions.BanAction { return useractions.BanAction{Reason: "test"} }
func UseBanAction() models.User { return models.User{} }
`
	if err := os.WriteFile(filepath.Join(dir, "ban.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "promote.go"), []byte(promote), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "standalone_gate.go"), []byte(standaloneGate), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "database", "policies", "2026_03_24_100000_grant_ban.go"), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "app", "services", "action_call.go"), []byte(callSite), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedEncryptionBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models_test

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"encryption-test/app/models"
)

func TestExportedEncryptedColumnsRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("APP_ENCRYPTION_KEY", key)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}

	user := &models.User{
		ID:         uuid.New(),
		Name:       "Ada",
		Email:      "ada@example.com",
		ApiKey:     "api-secret",
		PrivateKey: "private-secret",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if user.EmailEncrypted == "" || user.EmailEncrypted == user.Email {
		t.Fatalf("expected email ciphertext, got %q", user.EmailEncrypted)
	}
	if user.PrivateKeyEncrypted == "" || user.PrivateKeyEncrypted == user.PrivateKey {
		t.Fatalf("expected sealed private key ciphertext, got %q", user.PrivateKeyEncrypted)
	}

	var raw struct {
		EmailEnc   string
		PrivateEnc string
	}
	if err := db.Raw("SELECT email_encrypted, private_key_encrypted FROM users WHERE id = ?", user.ID).Scan(&raw).Error; err != nil {
		t.Fatalf("raw select: %v", err)
	}
	if raw.EmailEnc == "ada@example.com" || raw.PrivateEnc == "private-secret" {
		t.Fatalf("plaintext leaked into ciphertext columns: %#v", raw)
	}

	var found models.User
	if err := db.First(&found, "email_encrypted = ?", user.EmailEncrypted).Error; err != nil {
		t.Fatalf("find by encrypted email: %v", err)
	}
	if found.Email != "ada@example.com" || found.ApiKey != "api-secret" || found.PrivateKey != "private-secret" {
		t.Fatalf("decrypted fields mismatch: %#v", found)
	}

	other := &models.User{ID: uuid.New(), Name: "Grace", Email: "ada@example.com", ApiKey: "api-secret", PrivateKey: "private-secret"}
	if err := db.Create(other).Error; err != nil {
		t.Fatalf("second create: %v", err)
	}
	if other.EmailEncrypted != user.EmailEncrypted {
		t.Fatalf("encrypted email should be deterministic for equality search")
	}
	if other.PrivateKeyEncrypted == user.PrivateKeyEncrypted {
		t.Fatalf("sealed private key should be non-deterministic")
	}

	os.Unsetenv("APP_ENCRYPTION_KEY")
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_encryption_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedIntegrityBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"ledger/app/models"
)

func TestExportedIntegrityTablesPreserveBehavior(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.Account{}, &models.Transaction{}); err != nil {
		t.Fatal(err)
	}

	ownerID := uuid.New()
	account := &models.Account{
		OwnerID:  ownerID,
		Name:     "Checking",
		Currency: "USD",
		Type:     "checking",
		Active:   true,
	}
	if err := models.CreateAccount(account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if len(account.RowHash) != 32 || len(account.PrevHash) != 32 {
		t.Fatalf("account hashes were not populated: row=%d prev=%d", len(account.RowHash), len(account.PrevHash))
	}
	originalVersion := account.VersionID
	originalHash := append([]byte(nil), account.RowHash...)

	account.Name = "Updated Checking"
	if err := models.UpdateAccount(account); err != nil {
		t.Fatalf("update account: %v", err)
	}
	if account.VersionID == originalVersion {
		t.Fatal("immutable update should create a fresh version_id")
	}
	if !bytesEqual(account.PrevHash, originalHash) {
		t.Fatalf("updated account prev_hash should link to original row_hash")
	}
	var accountRows int64
	if err := db.Model(&models.Account{}).Where("id = ?", account.ID).Count(&accountRows).Error; err != nil {
		t.Fatal(err)
	}
	if accountRows != 2 {
		t.Fatalf("immutable update should insert a new version, got %d rows", accountRows)
	}
	if err := models.VerifyAccountChain(); err != nil {
		t.Fatalf("verify account chain: %v", err)
	}

	tx1 := &models.Transaction{AccountID: account.ID, Type: "credit", Amount: decimal.NewFromInt(100), Currency: "USD"}
	tx2 := &models.Transaction{AccountID: account.ID, Type: "debit", Amount: decimal.NewFromInt(25), Currency: "USD"}
	if err := models.CreateTransaction(tx1); err != nil {
		t.Fatalf("create tx1: %v", err)
	}
	if err := models.CreateTransaction(tx2); err != nil {
		t.Fatalf("create tx2: %v", err)
	}
	if len(tx1.RowHash) != 32 || len(tx2.RowHash) != 32 {
		t.Fatal("transaction hashes were not populated")
	}
	if !bytesEqual(tx2.PrevHash, tx1.RowHash) {
		t.Fatal("append-only transaction chain did not link to previous row")
	}
	if err := models.VerifyTransactionChain(); err != nil {
		t.Fatalf("verify transaction chain: %v", err)
	}

	if err := db.Model(&models.Transaction{}).Where("id = ?", tx1.ID).Update("amount", decimal.NewFromInt(999)).Error; err != nil {
		t.Fatalf("tamper transaction: %v", err)
	}
	if err := models.VerifyTransactionChain(); err == nil {
		t.Fatal("VerifyTransactionChain should detect tampering")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_integrity_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportLedgerCompiles(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "ledger"))
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	assertFileContains(t, filepath.Join(out, "app", "models", "transaction.go"), "decimal.Decimal")
	assertFileContains(t, filepath.Join(out, "app", "models", "account.go"), "RowHash")
	assertFileContains(t, filepath.Join(out, "app", "models", "account.go"), "[]byte")
	assertFileContains(t, filepath.Join(out, "app", "models", "integrity_support.go"), "func CreateAccount(record *Account) error")
	assertFileContains(t, filepath.Join(out, "app", "models", "integrity_support.go"), "func VerifyTransactionChain() error")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "account_controller.go"), "models.DB.Model(&models.Account{})")
	assertPathMissing(t, filepath.Join(out, "integrity_test.go"))
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "QueryAccount")
	writeExportedIntegrityBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportEncryptionLowersGORMHooks(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "encryption-test"))
	out := filepath.Join(t.TempDir(), "exported")
	res, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if hasFinding(res.Findings, "encrypted_columns") {
		t.Fatalf("did not expect encrypted_columns finding, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "func (m *User) Public() UserPublic")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "func PublicUsers(records []User) []UserPublic")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), `Email                 string    `+"`"+`json:"email" gorm:"-"`+"`")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "EmailEncrypted        string")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "func (m *User) BeforeSave(tx *gorm.DB) error")
	assertFileContains(t, filepath.Join(out, "app", "models", "encryption_support.go"), "func encryptDeterministic")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Encrypted and sealed columns with GORM encrypt/decrypt hooks")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	writeExportedEncryptionBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportZeroGraphQLLowersGraphQLPackage(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "zero-graphql"))
	out := filepath.Join(t.TempDir(), "exported")
	res, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if hasFinding(res.Findings, "generated_graphql") {
		t.Fatalf("did not expect generated_graphql finding, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func Handler() http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func QueryUser() *UserQuery")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), `mux.Handle("/graphql", graphql.Handler())`)
	assertFileContains(t, filepath.Join(out, "app", "http", "requests", "bindings.go"), "package requests")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	runExported(t, out, "go", "test", "./...")
}

func TestExportGraphQLSafetyLowersGraphQLPackage(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "graphql-safety"))
	out := filepath.Join(t.TempDir(), "exported")
	res, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if hasFinding(res.Findings, "generated_graphql") {
		t.Fatalf("did not expect generated_graphql finding, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "graphql", "schema_gen.go"), "type Query")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "pickle_gen.go"), "maxQueryComplexity")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func (q *UserQuery) WhereID")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `mux.Handle("/graphql", graphql.Handler())`)
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	runExported(t, out, "go", "test", "./...")
}

func TestExportMonorepoCompiles(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "monorepo"))
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	assertFileContains(t, filepath.Join(out, "services", "api", "http", "controllers", "order_controller.go"), "monorepo/internal/httpx")
	assertFileContains(t, filepath.Join(out, "services", "api", "http", "requests", "bindings.go"), "BindCreateOrderRequest")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "apiRoutes")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "workerRoutes")
	assertNoGoFileContains(t, out, "QueryOrder")
	runExported(t, out, "go", "test", "./...")
}

func writeExportedCronBehaviorTests(t *testing.T, out string) {
	t.Helper()
	jobsTest := `package jobs

import (
	"sync/atomic"
	"testing"
	"time"
)

type exportedFlakyJob struct {
	attempts int32
	failures int32
}

func (j *exportedFlakyJob) Handle() error {
	attempt := atomic.AddInt32(&j.attempts, 1)
	if attempt <= j.failures {
		return errExportedFlaky
	}
	return nil
}

var errExportedFlaky = &exportedCronError{}

type exportedCronError struct{}

func (*exportedCronError) Error() string { return "flaky" }

func TestExportedSchedulerOptionsAndRetries(t *testing.T) {
	job := &exportedFlakyJob{failures: 2}
	scheduler := Cron(func(s *Scheduler) {
		s.Job("*/5 * * * *", job).
			MaxRetries(2).
			RetryDelay(time.Millisecond).
			Timeout(time.Second).
			AllowOverlap()
	})
	if len(scheduler.Entries()) != 1 {
		t.Fatalf("expected one scheduled job, got %d", len(scheduler.Entries()))
	}
	entry := scheduler.Entries()[0]
	if entry.maxRetries != 2 {
		t.Fatalf("maxRetries = %d, want 2", entry.maxRetries)
	}
	if entry.retryDelay != time.Millisecond {
		t.Fatalf("retryDelay = %s, want 1ms", entry.retryDelay)
	}
	if entry.timeout != time.Second {
		t.Fatalf("timeout = %s, want 1s", entry.timeout)
	}
	if !entry.allowOverlap {
		t.Fatal("AllowOverlap should enable overlapping runs")
	}
	runJob(entry)
	if got := atomic.LoadInt32(&job.attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "jobs", "exported_scheduler_test.go"), []byte(jobsTest), 0o644); err != nil {
		t.Fatal(err)
	}

	scheduleTest := `package schedule_test

import (
	"testing"

	"cron-test/schedule"
)

func TestExportedScheduleRegistry(t *testing.T) {
	entries := schedule.Schedule.Entries()
	if len(entries) != 3 {
		t.Fatalf("expected three scheduled jobs, got %d", len(entries))
	}
	want := []string{"0 * * * *", "0 0 * * *", "*/5 * * * *"}
	for i, expected := range want {
		if entries[i].Schedule != expected {
			t.Fatalf("entry %d schedule = %q, want %q", i, entries[i].Schedule, expected)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "schedule", "exported_schedule_test.go"), []byte(scheduleTest), 0o644); err != nil {
		t.Fatal(err)
	}

	migrationsTest := `package migrations

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedSQLMigrationRunner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(db, "sqlite")
	if err := runner.Migrate(Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	statuses, err := runner.Status(Registry)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(statuses) != len(Registry) {
		t.Fatalf("statuses = %d, want %d", len(statuses), len(Registry))
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Fatalf("migration %s should be applied", status.ID)
		}
	}
	if err := runner.Rollback(Registry); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	statuses, err = runner.Status(Registry)
	if err != nil {
		t.Fatalf("status after rollback: %v", err)
	}
	for _, status := range statuses {
		if status.Applied {
			t.Fatalf("migration %s should be rolled back", status.ID)
		}
	}
	if err := runner.Fresh(Registry); err != nil {
		t.Fatalf("fresh: %v", err)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "migrations", "exported_migrations_test.go"), []byte(migrationsTest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportCronCompilesWithSchedulerSupport(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "cron-test"))
	out := filepath.Join(t.TempDir(), "exported")
	res, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if hasFinding(res.Findings, "generated_jobs") {
		t.Fatalf("did not expect generated_jobs finding, got %+v", res.Findings)
	}
	if hasFinding(res.Findings, "generated_commands") {
		t.Fatalf("did not expect generated_commands finding, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "jobs", "support.go"), "type Scheduler struct")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func NewApp() *App")
	assertFileContains(t, filepath.Join(out, "schedule", "jobs.go"), "jobs.Cron")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "go schedule.Schedule.Start(ctx)")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Cron job scheduler support with exported server startup wiring")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone command dispatch with embedded SQL migration commands")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "No unsupported export findings.")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	writeExportedCronBehaviorTests(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportRefusesNonEmptyOutputWithoutForce(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("expected non-empty output error, got %v", err)
	}
}

func TestExportFailsUnknownViewMigrations(t *testing.T) {
	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "2026_02_21_100000_create_active_users_view.go"), []byte("package migrations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := &exporter{project: &generator.Project{Layout: generator.Layout{MigrationsDir: migrationsDir}}}
	_, err := ex.generateSQLMigrations(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown view active_users") {
		t.Fatalf("expected unsupported view migration error, got %v", err)
	}
}

func TestExportFailsUnsupportedMigrationWithActionableKind(t *testing.T) {
	migrationsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationsDir, "2026_02_21_100000_add_email_to_users_table.go"), []byte("package migrations\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := &exporter{project: &generator.Project{Layout: generator.Layout{MigrationsDir: migrationsDir}}}
	_, err := ex.generateSQLMigrations(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "add-column/index migrations are not lowered yet") {
		t.Fatalf("expected actionable unsupported migration error, got %v", err)
	}
}

func TestGenerateSQLMigrationsLowersCapturedOperations(t *testing.T) {
	ex := &exporter{migrations: []generator.MigrationOps{
		{
			Name: "AddEmailToUsers_2026_02_21_100000",
			Up: []generator.MigrationOperation{
				{Type: "add_column", Table: "users", Columns: []*schema.Column{{Name: "email", Type: schema.String, Length: 255, IsUnique: true}}},
				{Type: "rename_column", Table: "users", OldName: "name", NewName: "full_name"},
				{Type: "add_unique_index", Table: "users", Index: &schema.Index{Table: "users", Columns: []string{"email"}, Unique: true}},
			},
			Down: []generator.MigrationOperation{
				{Type: "rename_column", Table: "users", OldName: "full_name", NewName: "name"},
				{Type: "drop_column", Table: "users", ColumnName: "email"},
			},
		},
	}}
	migrations, err := ex.generateSQLMigrations(nil, nil)
	if err != nil {
		t.Fatalf("generateSQLMigrations: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	if migrations[0].Name != "20260221100000_add_email_to_users" {
		t.Fatalf("migration name = %q", migrations[0].Name)
	}
	for _, want := range []string{
		`ALTER TABLE "users" ADD COLUMN "email" VARCHAR(255) NOT NULL UNIQUE`,
		`ALTER TABLE "users" RENAME COLUMN "name" TO "full_name"`,
		`CREATE UNIQUE INDEX "uidx_users_email" ON "users" ("email")`,
	} {
		if !strings.Contains(migrations[0].Up, want) {
			t.Fatalf("up migration missing %q:\n%s", want, migrations[0].Up)
		}
	}
	for _, want := range []string{
		`ALTER TABLE "users" RENAME COLUMN "full_name" TO "name"`,
		`ALTER TABLE "users" DROP COLUMN "email"`,
	} {
		if !strings.Contains(migrations[0].Down, want) {
			t.Fatalf("down migration missing %q:\n%s", want, migrations[0].Down)
		}
	}
}

func TestGenerateSQLMigrationsLowersRawSQLWithFinding(t *testing.T) {
	ex := &exporter{result: &Result{}, migrations: []generator.MigrationOps{
		{
			Name: "SeedUsers_2026_02_21_100000",
			Up: []generator.MigrationOperation{
				{Type: "raw_sql", SQL: "INSERT INTO users (id, name) VALUES (1, 'admin');"},
			},
			Down: []generator.MigrationOperation{
				{Type: "raw_sql", SQL: "DELETE FROM users WHERE id = 1;"},
			},
		},
	}}
	migrations, err := ex.generateSQLMigrations(nil, nil)
	if err != nil {
		t.Fatalf("generateSQLMigrations: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("got %d migrations, want 1", len(migrations))
	}
	if !strings.Contains(migrations[0].Up, "INSERT INTO users") {
		t.Fatalf("up migration missing raw SQL:\n%s", migrations[0].Up)
	}
	if !strings.Contains(migrations[0].Down, "DELETE FROM users") {
		t.Fatalf("down migration missing raw SQL:\n%s", migrations[0].Down)
	}
	if !hasFinding(ex.result.Findings, "raw_sql_migration") {
		t.Fatalf("expected raw_sql_migration finding, got %+v", ex.result.Findings)
	}
}

func TestRewriteMutableQueryVariable(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Index(role string) ([]models.User, error) {
	q := models.QueryUser()
	q.WhereRole(role)
	q.OrderByID("ASC")
	return q.All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	for _, want := range []string{
		"q := models.DB.Model(&models. User{})",
		`q = q.Where("role = ?", role, )`,
		`q = q.Order("id" + " " + "ASC")`,
		"return func() ([]models.User, error)",
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("rewritten source missing %q:\n%s", want, got)
		}
	}
}

func TestPascalToSnakePreservesCommonInitialisms(t *testing.T) {
	cases := map[string]string{
		"ID":        "id",
		"OwnerID":   "owner_id",
		"UserID":    "user_id",
		"AccountID": "account_id",
		"URLValue":  "url_value",
		"APIKey":    "api_key",
	}
	for input, want := range cases {
		if got := pascalToSnake(input); got != want {
			t.Fatalf("pascalToSnake(%q) = %q, want %q", input, got, want)
		}
	}
}

func hasFinding(findings []Finding, rule string) bool {
	for _, finding := range findings {
		if finding.Rule == rule {
			return true
		}
	}
	return false
}

func copyProject(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "project")
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == ".pickle-tmp" {
				return filepath.SkipDir
			}
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected %s to contain %q", path, want)
	}
}

func assertFileNotContains(t *testing.T, path, needle string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), needle) {
		t.Fatalf("expected %s not to contain %q", path, needle)
	}
}

func assertNoGoFileContains(t *testing.T, root, needle string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), needle) {
			t.Fatalf("%s contains %q", path, needle)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s to be absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func runExported(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}
