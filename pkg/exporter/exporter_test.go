package exporter

import (
	"fmt"
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
	if hasFinding(res.Findings, "rbac_policy_export") {
		t.Fatalf("did not expect rbac_policy_export finding, got %+v", res.Findings)
	}
	if hasFinding(res.Findings, "generated_graphql_policies") {
		t.Fatalf("did not expect generated_graphql_policies finding, got %+v", res.Findings)
	}
	if hasFinding(res.Findings, "actions_audit") {
		t.Fatalf("did not expect actions_audit finding, got %+v", res.Findings)
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
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone JWT, OAuth client-credentials, and session auth drivers")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone RBAC and GraphQL policy state support with changelog tables")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Unsupported")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "No unsupported export findings.")
	assertFileNotContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Manual Review")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func Env(key, fallback string) string")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "type ConnectionConfig struct")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func OpenGORM(conn ConnectionConfig) *gorm.DB")
	assertFileContains(t, filepath.Join(out, "config", "app.go"), "func app() AppConfig")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func BuiltinCommands() []Command")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
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
	assertFileContains(t, filepath.Join(out, "app", "models", "action_audit_support.go"), "func runAuditedAction")
	assertFileContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "func LoadRoles")
	assertFileContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "func RequireRole")
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
	writeExportedSessionCSRFBehaviorTest(t, out)
	writeExportedConfigBehaviorTest(t, out)
	writeExportedActionAuditBehaviorTest(t, out)
	writeExportedMigrationBehaviorTest(t, out)
	writeExportedPolicyBehaviorTest(t, out)
	writeExportedRouterMiddlewareBehaviorTest(t, out)
	writeExportedRBACMiddlewareBehaviorTest(t, out)
	writeExportedAuthPlaceholderTests(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedConfigBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package config

import (
	"strings"
	"testing"
)

func TestConnectionConfigRejectsUnsupportedDriversWithoutPanic(t *testing.T) {
	conn := ConnectionConfig{Driver: "oracle", Name: "ignored"}
	if err := conn.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported database driver: oracle") {
		t.Fatalf("Validate() error = %v, want unsupported driver", err)
	}
	if got := conn.DSN(); got != "" {
		t.Fatalf("unsupported DSN = %q, want empty string", got)
	}
	if _, err := TryOpenDB(conn); err == nil || !strings.Contains(err.Error(), "unsupported database driver: oracle") {
		t.Fatalf("TryOpenDB() error = %v, want unsupported driver", err)
	}
	if _, err := TryOpenGORM(conn); err == nil || !strings.Contains(err.Error(), "unsupported database driver: oracle") {
		t.Fatalf("TryOpenGORM() error = %v, want unsupported driver", err)
	}
}

func TestConnectionConfigTryOpenGORM(t *testing.T) {
	db, err := TryOpenGORM(ConnectionConfig{Driver: "sqlite", Name: ":memory:"})
	if err != nil {
		t.Fatalf("TryOpenGORM(sqlite): %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseConfigRejectsUnknownConnectionsWithoutFatal(t *testing.T) {
	cfg := DatabaseConfig{
		Default: "sqlite",
		Connections: map[string]ConnectionConfig{
			"sqlite": {Driver: "sqlite", Name: ":memory:"},
		},
	}
	conn, err := cfg.TryConnection()
	if err != nil {
		t.Fatalf("TryConnection(default): %v", err)
	}
	if conn.Driver != "sqlite" {
		t.Fatalf("default connection = %#v", conn)
	}
	if _, err := cfg.TryConnection("missing"); err == nil || !strings.Contains(err.Error(), "unknown database connection: missing") {
		t.Fatalf("TryConnection(missing) error = %v, want unknown connection", err)
	}
	if _, err := cfg.TryOpen("missing"); err == nil || !strings.Contains(err.Error(), "unknown database connection: missing") {
		t.Fatalf("TryOpen(missing) error = %v, want unknown connection", err)
	}
	if _, err := cfg.TryOpenGORM("missing"); err == nil || !strings.Contains(err.Error(), "unknown database connection: missing") {
		t.Fatalf("TryOpenGORM(missing) error = %v, want unknown connection", err)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "config", "exported_config_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
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
	"basic-crud/internal/httpx"
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

	badEnv := func(key, fallback string) string {
		if key == "AUTH_DRIVER" {
			return "bogus"
		}
		return env(key, fallback)
	}
	auth.Init(badEnv, db)
	if _, err := auth.TryActiveDriver(); err == nil {
		t.Fatal("TryActiveDriver should reject unknown AUTH_DRIVER")
	}
	if _, err := auth.Authenticate(req); err == nil {
		t.Fatal("Authenticate should return an error for unknown AUTH_DRIVER")
	}
	resp := auth.DefaultAuthMiddleware(httpx.NewContext(req), func() httpx.Response {
		t.Fatal("middleware should not call next for unknown AUTH_DRIVER")
		return httpx.Response{}
	})
	if resp.Status != 401 {
		t.Fatalf("middleware status = %d, want 401", resp.Status)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "http", "auth", "exported_auth_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedAuthPlaceholderTests(t *testing.T, out string) {
	t.Helper()
	testSrc := `package %s

import "testing"

func TestBindPlaceholdersMatchesDriver(t *testing.T) {
	query := "INSERT INTO tokens (a, b, c) VALUES (?, ?, ?)"
	if got := bindPlaceholders("sqlite", query); got != query {
		t.Fatalf("sqlite placeholders = %%q", got)
	}
	if got := bindPlaceholders("mysql", query); got != query {
		t.Fatalf("mysql placeholders = %%q", got)
	}
	if got := bindPlaceholders("postgres", query); got != "INSERT INTO tokens (a, b, c) VALUES ($1, $2, $3)" {
		t.Fatalf("postgres placeholders = %%q", got)
	}
	if got := bindPlaceholders("pgsql", "SELECT * FROM tokens WHERE a = ? AND b = ?"); got != "SELECT * FROM tokens WHERE a = $1 AND b = $2" {
		t.Fatalf("pgsql placeholders = %%q", got)
	}
}
`
	for _, pkg := range []struct {
		Dir  string
		Name string
	}{
		{filepath.Join("app", "http", "auth", "jwt"), "jwt"},
		{filepath.Join("app", "http", "auth", "oauth"), "oauth"},
		{filepath.Join("app", "http", "auth", "session"), "session"},
	} {
		path := filepath.Join(out, pkg.Dir, "exported_placeholders_test.go")
		if err := os.WriteFile(path, []byte(fmt.Sprintf(testSrc, pkg.Name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeExportedSessionCSRFBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package session

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"basic-crud/internal/httpx"
)

func TestExportedSessionCSRFBoundary(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	env := func(key, fallback string) string {
		values := map[string]string{
			"SESSION_SECRET": "session-secret",
			"SESSION_COOKIE": "sid",
			"CSRF_COOKIE": "csrf_token",
		}
		if value, ok := values[key]; ok {
			return value
		}
		return fallback
	}
	NewDriver(env, db, "sqlite")

	getCtx := httpx.NewContext(requestWithSession(http.MethodGet, "sess-1"))
	getResp := CSRF(getCtx, func() httpx.Response {
		return getCtx.NoContent()
	})
	if getResp.StatusCode != http.StatusNoContent {
		t.Fatalf("GET status = %d", getResp.StatusCode)
	}
	if cookie := findCookie(getResp.Cookies, "csrf_token"); cookie == nil {
		t.Fatal("safe request should receive a CSRF cookie")
	} else if cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("csrf cookie security attributes = %#v", cookie)
	}

	postCtx := httpx.NewContext(requestWithSession(http.MethodPost, "sess-1"))
	missing := CSRF(postCtx, func() httpx.Response {
		t.Fatal("CSRF should block missing token")
		return httpx.Response{}
	})
	if missing.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token status = %d", missing.StatusCode)
	}

	invalidReq := requestWithSession(http.MethodPost, "sess-1")
	invalidReq.Header.Set("X-CSRF-TOKEN", "bogus.token")
	invalidCtx := httpx.NewContext(invalidReq)
	invalid := CSRF(invalidCtx, func() httpx.Response {
		t.Fatal("CSRF should block invalid token")
		return httpx.Response{}
	})
	if invalid.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid token status = %d", invalid.StatusCode)
	}

	validReq := requestWithSession(http.MethodPost, "sess-1")
	validReq.Header.Set("X-CSRF-TOKEN", generateCSRFToken("sess-1", csrfConfig.secret))
	validCtx := httpx.NewContext(validReq)
	valid := CSRF(validCtx, func() httpx.Response {
		return validCtx.NoContent()
	})
	if valid.StatusCode != http.StatusNoContent {
		t.Fatalf("valid token status = %d", valid.StatusCode)
	}

	bearerReq := requestWithSession(http.MethodPost, "sess-1")
	bearerReq.Header.Set("Authorization", "Bearer api-token")
	bearerCtx := httpx.NewContext(bearerReq)
	bearer := CSRF(bearerCtx, func() httpx.Response {
		return bearerCtx.NoContent()
	})
	if bearer.StatusCode != http.StatusNoContent {
		t.Fatalf("bearer token bypass status = %d", bearer.StatusCode)
	}
}

func TestExportedSessionCreateSetsSessionAndCSRFCookies(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, db, "sqlite")

	ctx := httpx.NewContext(httptest.NewRequest(http.MethodPost, "/login", nil))
	resp, err := Create(ctx, "user-1", "member")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if findCookie(resp.Cookies, sessionCookieName) == nil {
		t.Fatal("session cookie should be set")
	}
	if findCookie(resp.Cookies, csrfConfig.cookieName) == nil {
		t.Fatal("csrf cookie should be set")
	}
}

func TestExportedCSRFRequiresConfiguredSecretWithoutPanic(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return ""
		}
		return fallback
	}, db, "sqlite")
	if len(csrfConfig.secret) != 0 {
		t.Fatal("missing SESSION_SECRET should clear CSRF secret")
	}
	ctx := httpx.NewContext(requestWithSession(http.MethodPost, "sess-1"))
	resp := CSRF(ctx, func() httpx.Response {
		t.Fatal("CSRF should not call next when secret is missing")
		return httpx.Response{}
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing secret status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func requestWithSession(method, sessionID string) *http.Request {
	req := httptest.NewRequest(method, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	return req
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "http", "auth", "session", "exported_csrf_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedActionAuditBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models_test

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"basic-crud/app/models"
	"basic-crud/internal/httpx"
)

func TestExportedActionsPersistAuditRowsTransactionally(t *testing.T) {
	t.Setenv("APP_ENCRYPTION_KEY", "12345678901234567890123456789012")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.Exec(` + "`" + `CREATE TABLE users (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		email_encrypted TEXT NOT NULL,
		email_encrypted_v2 TEXT,
		password_hash_encrypted TEXT NOT NULL,
		password_hash_encrypted_v2 TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)` + "`" + `).Error; err != nil {
		t.Fatal(err)
	}
	userID := uuid.New()
	if err := db.Exec("INSERT INTO users (id, name, email_encrypted, password_hash_encrypted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", userID.String(), "before", "email", "pw", time.Now(), time.Now()).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/users/ban", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.RemoteAddr = "192.0.2.10:1234"
	ctx := httpx.NewContext(req)
	ctx.SetAuth(&httpx.AuthInfo{UserID: uuid.New().String(), Role: "admin"})

	user := &models.User{ID: userID, Name: "before"}
	if err := user.Ban(ctx, models.BanAction{Reason: "banned"}); err != nil {
		t.Fatalf("ban: %v", err)
	}
	var auditRows int64
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows after successful action = %d, want 1", auditRows)
	}
	var actionName string
	if err := db.Raw("SELECT at.name FROM user_actions ua JOIN action_types at ON at.id = ua.action_type_id").Scan(&actionName).Error; err != nil {
		t.Fatal(err)
	}
	if actionName != "Ban" {
		t.Fatalf("audit action name = %q, want Ban", actionName)
	}

	deniedCtx := httpx.NewContext(req)
	deniedCtx.SetAuth(&httpx.AuthInfo{UserID: uuid.New().String(), Role: "viewer"})
	if err := user.Ban(deniedCtx, models.BanAction{Reason: "denied"}); !errors.Is(err, models.ErrUnauthorized) {
		t.Fatalf("denied ban error = %v, want ErrUnauthorized", err)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows after denied action = %d, want 1", auditRows)
	}

	if err := user.Fail(ctx, models.FailAction{}); err == nil {
		t.Fatal("expected failed action error")
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows after failed action = %d, want 1", auditRows)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_action_audit_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlTestSrc := `package models

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestActionAuditUpsertsMatchDialect(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := actionAuditModelTypeUpsertSQL(sqliteDB); !strings.Contains(got, "ON CONFLICT(id)") || strings.Contains(got, "ON DUPLICATE KEY") {
		t.Fatalf("sqlite model type upsert = %q", got)
	}
	if got := actionAuditModelTypeUpsertSQLForDialect("mysql"); !strings.Contains(got, "ON DUPLICATE KEY UPDATE") || strings.Contains(got, "excluded.") {
		t.Fatalf("mysql model type upsert = %q", got)
	}
	if got := actionAuditActionTypeUpsertSQLForDialect("mysql"); !strings.Contains(got, "VALUES(model_type_id)") || strings.Contains(got, "excluded.") {
		t.Fatalf("mysql action type upsert = %q", got)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_action_audit_sql_test.go"), []byte(sqlTestSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedMigrationBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package migrations_test

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"basic-crud/database/migrations"
)

func TestExportedMigrationsApplyToSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.NewRunner(db, "sqlite").Migrate(migrations.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	statuses, err := migrations.NewRunner(db, "sqlite").Status(migrations.Registry)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(statuses) != len(migrations.Registry) {
		t.Fatalf("statuses = %d, want %d", len(statuses), len(migrations.Registry))
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Fatalf("migration status %#v should be applied", status)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "migrations", "exported_migration_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedPolicyBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package policies

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedPolicyStateSupport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}

	var roles int64
	if err := db.Table("roles").Count(&roles).Error; err != nil {
		t.Fatal(err)
	}
	if roles != 3 {
		t.Fatalf("roles = %d, want 3", roles)
	}
	var adminCreates int64
	if err := db.Table("role_actions").Where("role_slug = ? AND action = ?", "admin", "users.create").Count(&adminCreates).Error; err != nil {
		t.Fatal(err)
	}
	if adminCreates != 1 {
		t.Fatalf("admin users.create grants = %d, want 1", adminCreates)
	}
	var userList int64
	if err := db.Table("graphql_exposures").Where("model = ? AND operation = ?", "users", "list").Count(&userList).Error; err != nil {
		t.Fatal(err)
	}
	if userList != 1 {
		t.Fatalf("graphql users.list exposures = %d, want 1", userList)
	}
	statuses, err := Status(db, "sqlite")
	if err != nil {
		t.Fatalf("policy status: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("expected policy statuses")
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Fatalf("policy status %#v should be applied", status)
		}
	}
	if err := Rollback(db, "sqlite"); err != nil {
		t.Fatalf("policy rollback: %v", err)
	}
	if err := db.Table("roles").Count(&roles).Error; err != nil {
		t.Fatal(err)
	}
	if roles != 0 {
		t.Fatalf("roles after rollback = %d, want 0", roles)
	}
}

func TestExportedPolicyUpsertsMatchDialect(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := roleUpsertSQL(sqliteDB); !strings.Contains(got, "ON CONFLICT(slug)") || strings.Contains(got, "ON DUPLICATE KEY") {
		t.Fatalf("sqlite role upsert = %q", got)
	}
	if got := roleUpsertSQLForDialect("mysql"); !strings.Contains(got, "ON DUPLICATE KEY UPDATE") || strings.Contains(got, "excluded.") {
		t.Fatalf("mysql role upsert = %q", got)
	}
	if got := policyAppliedUpsertSQLForDialect("mysql", "rbac_changelog"); !strings.Contains(got, "ON DUPLICATE KEY UPDATE") || strings.Contains(got, "excluded.") {
		t.Fatalf("mysql policy applied upsert = %q", got)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "policies", "exported_policy_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedRouterMiddlewareBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"basic-crud/internal/httpx"
)

func TestExportedRouterRunsMiddlewareInDeclaredOrder(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	var order []string
	groupMiddleware := func(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
		order = append(order, "group")
		return next()
	}
	routeMiddleware := func(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
		order = append(order, "route")
		return next()
	}
	router := httpx.Routes(func(r *httpx.Router) {
		r.Group("/api", groupMiddleware, func(r *httpx.Router) {
			r.Get("/users/:id", func(ctx *httpx.Context) httpx.Response {
				order = append(order, "handler:"+ctx.Param("id"))
				return ctx.JSON(http.StatusAccepted, map[string]string{"ok": "true"})
			}, routeMiddleware)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/users/42", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	want := []string{"group", "route", "handler:42"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("middleware order = %#v, want %#v", order, want)
	}
}

func TestExportedGroupMiddlewareAfterBodyAppliesToRoutes(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	var order []string
	authMiddleware := func(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
		order = append(order, "auth")
		return next()
	}
	router := httpx.Routes(func(r *httpx.Router) {
		r.Group("/api", func(r *httpx.Router) {
			r.Get("/secure", func(ctx *httpx.Context) httpx.Response {
				order = append(order, "handler")
				return ctx.NoContent()
			})
		}, authMiddleware)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/secure", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	want := []string{"auth", "handler"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("middleware order = %#v, want %#v", order, want)
	}
}

func TestExportedResourceRoutesAndContextHelpers(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	id := uuid.New()
	controller := &resourceController{}
	router := httpx.Routes(func(r *httpx.Router) {
		r.Resource("/posts", controller)
	})

	cases := []struct {
		method string
		path   string
		want   string
		status int
	}{
		{http.MethodGet, "/posts?filter=recent", "index:recent", http.StatusOK},
		{http.MethodGet, "/posts/" + id.String(), "show:" + id.String(), http.StatusOK},
		{http.MethodPost, "/posts", "store", http.StatusCreated},
		{http.MethodPut, "/posts/" + id.String(), "update:" + id.String(), http.StatusAccepted},
		{http.MethodDelete, "/posts/" + id.String(), "destroy:" + id.String(), http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "flavor", Value: "dill"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if controller.last != tc.want {
				t.Fatalf("last handler = %q, want %q", controller.last, tc.want)
			}
			if controller.sawWriter == false {
				t.Fatal("ResponseWriter should be available inside routed contexts")
			}
			if controller.cookie != "dill" {
				t.Fatalf("cookie = %q, want dill", controller.cookie)
			}
		})
	}
}

func TestExportedAllRoutesAndRegisterRoutes(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	router := httpx.Routes(func(r *httpx.Router) {
		r.Group("/api", func(r *httpx.Router) {
			r.Get("/health", func(ctx *httpx.Context) httpx.Response {
				return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
			})
			r.Get("/users/:id", func(ctx *httpx.Context) httpx.Response {
				return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Param("id")})
			})
		})
	})
	routes := router.AllRoutes()
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(routes))
	}
	if routes[0].Method != http.MethodGet || routes[0].Path != "/api/health" {
		t.Fatalf("route[0] = %#v", routes[0])
	}

	mux := http.NewServeMux()
	router.RegisterRoutes(mux)
	for _, path := range []string{"/api/health", "/api/health/"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/42", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "42") {
		t.Fatalf("param route status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExportedRegisterRoutesDuplicatePanics(t *testing.T) {
	router := httpx.Routes(func(r *httpx.Router) {
		r.Get("/dup", func(ctx *httpx.Context) httpx.Response { return ctx.NoContent() })
		r.Get("/dup", func(ctx *httpx.Context) httpx.Response { return ctx.NoContent() })
	})
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("duplicate route registration should panic")
		}
	}()
	router.RegisterRoutes(http.NewServeMux())
}

func TestExportedOnErrorReceivesRecoveredPanic(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	var reported error
	router := httpx.Routes(func(r *httpx.Router) {
		r.OnError(func(ctx *httpx.Context, err error) {
			reported = err
			if ctx == nil || ctx.Param("id") != "123" {
				t.Fatalf("reported context = %#v", ctx)
			}
		})
		r.Get("/panic/:id", func(ctx *httpx.Context) httpx.Response {
			panic("boom")
		})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic/123", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if reported == nil || reported.Error() != "boom" {
		t.Fatalf("reported error = %v", reported)
	}
}

func TestExportedContextResourceHelpersPropagateOwner(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := httpx.NewContext(req)
	ctx.SetAuth(&httpx.AuthInfo{UserID: "owner-1"})

	one := &resourceQuery{value: map[string]string{"id": "one"}}
	resp := ctx.Resource(one)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Resource status = %d", resp.StatusCode)
	}
	if one.ownerID != "owner-1" {
		t.Fatalf("resource owner = %q", one.ownerID)
	}

	list := &resourceListQuery{value: []string{"a", "b"}}
	resp = ctx.Resources(list)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Resources status = %d", resp.StatusCode)
	}
	if list.ownerID != "owner-1" {
		t.Fatalf("resources owner = %q", list.ownerID)
	}

	missing := &resourceQuery{err: errors.New("sql: no rows in result set")}
	resp = ctx.Resource(missing)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing resource status = %d", resp.StatusCode)
	}
}

func TestExportedRateLimitMiddlewareDeniesAfterBurst(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	router := httpx.Routes(func(r *httpx.Router) {
		r.Get("/limited", func(ctx *httpx.Context) httpx.Response {
			return ctx.NoContent()
		}, httpx.RateLimit(1, 1))
	})

	first := httptest.NewRequest(http.MethodGet, "/limited", nil)
	first.RemoteAddr = "192.0.2.10:1234"
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, body = %s", firstRec.Code, firstRec.Body.String())
	}
	if firstRec.Header().Get("X-RateLimit-Limit") == "" {
		t.Fatal("expected rate limit headers on allowed response")
	}

	second := httptest.NewRequest(http.MethodGet, "/limited", nil)
	second.RemoteAddr = "192.0.2.10:1234"
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, body = %s", secondRec.Code, secondRec.Body.String())
	}
	if secondRec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on denied response")
	}
}

func TestExportedAuthRateLimitProviderSupportsTiers(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	var events []httpx.RateLimitEvent
	limiter := httpx.AuthRateLimit().RPS(100).Burst(10).Tiers(map[string]httpx.RateTier{
		"free":  {RPS: 100, Burst: 1},
		"admin": {RPS: 100, Burst: 2},
	})
	router := httpx.Routes(func(r *httpx.Router) {
		r.OnRateLimit(func(ctx *httpx.Context, event httpx.RateLimitEvent) {
			events = append(events, event)
		})
		r.Get("/identity", func(ctx *httpx.Context) httpx.Response {
			return ctx.NoContent()
		}, func(ctx *httpx.Context, next func() httpx.Response) httpx.Response {
			ctx.SetAuth(&httpx.AuthInfo{UserID: "user-1", Role: ctx.Request().Header.Get("X-Role")})
			return next()
		}, limiter)
	})

	first := httptest.NewRequest(http.MethodGet, "/identity", nil)
	first.Header.Set("X-Role", "free")
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first free status = %d", firstRec.Code)
	}

	second := httptest.NewRequest(http.MethodGet, "/identity", nil)
	second.Header.Set("X-Role", "free")
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("second free status = %d", secondRec.Code)
	}

	admin := httptest.NewRequest(http.MethodGet, "/identity", nil)
	admin.Header.Set("X-Role", "admin")
	adminRec := httptest.NewRecorder()
	router.ServeHTTP(adminRec, admin)
	if adminRec.Code != http.StatusNoContent {
		t.Fatalf("admin tier should use a separate bucket, got %d", adminRec.Code)
	}
	if len(events) != 3 {
		t.Fatalf("rate limit events = %d, want 3: %#v", len(events), events)
	}
	if events[0].Layer != "auth" || events[0].Key != "free:user-1" || !events[0].Allowed {
		t.Fatalf("event[0] = %#v", events[0])
	}
	if events[1].Layer != "auth" || events[1].Key != "free:user-1" || events[1].Allowed {
		t.Fatalf("event[1] = %#v", events[1])
	}
	if events[2].Layer != "auth" || events[2].Key != "admin:user-1" || !events[2].Allowed {
		t.Fatalf("event[2] = %#v", events[2])
	}
}

type resourceController struct {
	last      string
	cookie    string
	sawWriter bool
}

func (c *resourceController) capture(ctx *httpx.Context) {
	c.sawWriter = ctx.ResponseWriter() != nil
	c.cookie, _ = ctx.Cookie("flavor")
}

func (c *resourceController) Index(ctx *httpx.Context) httpx.Response {
	c.capture(ctx)
	c.last = "index:" + ctx.Query("filter")
	return ctx.JSON(http.StatusOK, nil)
}

func (c *resourceController) Show(ctx *httpx.Context) httpx.Response {
	c.capture(ctx)
	id, err := ctx.ParamUUID("id")
	if err != nil {
		return ctx.BadRequest(err.Error())
	}
	c.last = "show:" + id.String()
	return ctx.JSON(http.StatusOK, nil)
}

func (c *resourceController) Store(ctx *httpx.Context) httpx.Response {
	c.capture(ctx)
	c.last = "store"
	return ctx.JSON(http.StatusCreated, nil)
}

func (c *resourceController) Update(ctx *httpx.Context) httpx.Response {
	c.capture(ctx)
	c.last = "update:" + ctx.Param("id")
	return ctx.JSON(http.StatusAccepted, nil)
}

func (c *resourceController) Destroy(ctx *httpx.Context) httpx.Response {
	c.capture(ctx)
	c.last = "destroy:" + ctx.Param("id")
	return ctx.NoContent()
}

type resourceQuery struct {
	ownerID string
	value   any
	err     error
}

func (q *resourceQuery) FetchResource(ownerID string) (any, error) {
	q.ownerID = ownerID
	return q.value, q.err
}

type resourceListQuery struct {
	ownerID string
	value   any
	err     error
}

func (q *resourceListQuery) FetchResources(ownerID string) (any, error) {
	q.ownerID = ownerID
	return q.value, q.err
}
`
	if err := os.WriteFile(filepath.Join(out, "internal", "httpx", "exported_router_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	proxyTestSrc := `package httpx

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestExportedRateLimitIgnoresSpoofedProxyHeadersByDefault(t *testing.T) {
	resetTrustedProxyStateForTest()
	t.Setenv("TRUSTED_PROXIES", "")
	limiter := RateLimit(1, 1)
	handler := func() Response { return Response{StatusCode: http.StatusNoContent} }

	first := NewContext(requestFrom("10.0.0.1:1234", "198.51.100.1"))
	if resp := limiter(first, handler); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first status = %d", resp.StatusCode)
	}
	second := NewContext(requestFrom("10.0.0.1:1234", "198.51.100.2"))
	if resp := limiter(second, handler); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Forwarded-For should still share the remote bucket, got %d", resp.StatusCode)
	}
}

func TestExportedRateLimitHonorsTrustedProxyHeaders(t *testing.T) {
	resetTrustedProxyStateForTest()
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")
	limiter := RateLimit(1, 1)
	handler := func() Response { return Response{StatusCode: http.StatusNoContent} }

	first := NewContext(requestFrom("10.0.0.1:1234", "198.51.100.1"))
	if resp := limiter(first, handler); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first status = %d", resp.StatusCode)
	}
	second := NewContext(requestFrom("10.0.0.1:1234", "198.51.100.2"))
	if resp := limiter(second, handler); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("trusted proxy should key by client IP, got %d", resp.StatusCode)
	}
}

func requestFrom(remote, xff string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	req.Header.Set("X-Forwarded-For", xff)
	return req
}

func resetTrustedProxyStateForTest() {
	trustedProxies = nil
	trustedProxiesAll = false
	trustedProxiesOnce = sync.Once{}
}
`
	if err := os.WriteFile(filepath.Join(out, "internal", "httpx", "exported_proxy_test.go"), []byte(proxyTestSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedRBACMiddlewareBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package middleware_test

import (
	"net/http"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"basic-crud/app/http/middleware"
	"basic-crud/app/models"
	"basic-crud/database/policies"
	"basic-crud/internal/httpx"
)

func TestExportedRBACMiddlewareLoadsRolesAndEnforcesChecks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := policies.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}

	var adminID string
	if err := db.Raw("SELECT id FROM roles WHERE slug = ?", "admin").Scan(&adminID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := db.Exec("INSERT INTO role_user (user_id, role_id, created_at, updated_at) VALUES (?, ?, ?, ?)", "user-1", adminID, now, now).Error; err != nil {
		t.Fatal(err)
	}

	ctx := httpx.NewContext(newRequest())
	ctx.SetAuth(&httpx.AuthInfo{UserID: "user-1"})
	loaded := false
	resp := middleware.LoadRoles(ctx, func() httpx.Response {
		loaded = true
		if !ctx.HasAnyRole("admin") {
			t.Fatalf("loaded roles = %#v", ctx.Roles())
		}
		if !ctx.IsAdmin() {
			t.Fatal("admin role should set admin access through manages")
		}
		return ctx.NoContent()
	})
	if resp.StatusCode != http.StatusNoContent || !loaded {
		t.Fatalf("LoadRoles response = %#v loaded=%v", resp, loaded)
	}

	allowed := middleware.RequireRole("admin")(ctx, func() httpx.Response {
		return ctx.JSON(http.StatusAccepted, nil)
	})
	if allowed.StatusCode != http.StatusAccepted {
		t.Fatalf("RequireRole(admin) status = %d", allowed.StatusCode)
	}
	denied := middleware.RequireRole("editor")(ctx, func() httpx.Response {
		t.Fatal("RequireRole(editor) should not call next")
		return ctx.NoContent()
	})
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("RequireRole(editor) status = %d", denied.StatusCode)
	}
	admin := middleware.RequireAdmin(ctx, func() httpx.Response {
		return ctx.JSON(http.StatusCreated, nil)
	})
	if admin.StatusCode != http.StatusCreated {
		t.Fatalf("RequireAdmin status = %d", admin.StatusCode)
	}
}

func TestExportedLoadRolesRequiresAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	resp := middleware.LoadRoles(httpx.NewContext(newRequest()), func() httpx.Response {
		t.Fatal("LoadRoles should not call next without auth")
		return httpx.Response{}
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func newRequest() *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	return req
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "http", "middleware", "exported_rbac_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTableSQLUsesTableLevelCompositePrimaryKeyOnly(t *testing.T) {
	table := &schema.Table{Name: "transfers"}
	table.Immutable()
	table.String("status", 50).NotNull()

	sql := createTableSQL(table)
	if strings.Contains(sql, `"id" TEXT PRIMARY KEY`) {
		t.Fatalf("composite key column should not include inline primary key:\n%s", sql)
	}
	if strings.Contains(sql, `"version_id" TEXT PRIMARY KEY`) {
		t.Fatalf("composite key column should not include inline primary key:\n%s", sql)
	}
	if !strings.Contains(sql, `PRIMARY KEY ("id", "version_id")`) {
		t.Fatalf("missing table-level composite primary key:\n%s", sql)
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
	fail := `package user

import (
	"errors"
	models "github.com/shortontech/pickle/testdata/basic-crud/app/models"
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type FailAction struct{}

func (a FailAction) Fail(ctx *pickle.Context, user *models.User) error {
	return errors.New("boom")
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

func (m *GrantBan_2026_03_24_100000) Up() { m.AlterRole("admin").Can("Ban", "Promote", "Fail") }
func (m *GrantBan_2026_03_24_100000) Down() { m.AlterRole("admin").RevokeCan("Ban", "Promote", "Fail") }
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
	if err := os.WriteFile(filepath.Join(dir, "fail.go"), []byte(fail), 0o644); err != nil {
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
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
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
	assertFileContains(t, filepath.Join(out, "app", "graphql", "pickle_gen.go"), "var allowIntrospection = false")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func (q *UserQuery) WhereID")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `mux.Handle("/graphql", graphql.Handler())`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "routes.API.RegisterRoutes(mux)")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	writeExportedGraphQLSafetyBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedGraphQLSafetyBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphql_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"graphql-safety/app/graphql"
	"graphql-safety/app/models"
)

func TestExportedGraphQLSafetyCorpus(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	for _, stmt := range []string{
		` + "`" + `CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL, password_hash TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE posts (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT NOT NULL, user_id TEXT NOT NULL, body TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	userID := uuid.New()
	if err := db.Exec("INSERT INTO users (id, name, email, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", userID.String(), "Ada", "ada@example.com", "hash", now, now).Error; err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		query     string
		wantError bool
	}{
		{"allowed", ` + "`" + `query AllowedUsers {
  users(page: { first: 25 }) {
    edges { node { id name } }
    pageInfo { hasNextPage }
  }
}` + "`" + `, false},
		{"huge_first", ` + "`" + `query HugeFirst {
  users(page: { first: 101 }) { edges { node { id } } }
}` + "`" + `, true},
		{"introspection_disabled", ` + "`" + `query IntrospectionDisabled {
  __schema { queryType { name } }
}` + "`" + `, true},
		{"multi_operation", ` + "`" + `query One { users { edges { node { id } } } }
query Two { posts { edges { node { id } } } }` + "`" + `, true},
		{"repeated_aliases", ` + "`" + `query RepeatedAliases {
  a1: users { edges { node { id } } }
  a2: users { edges { node { id } } }
  a3: users { edges { node { id } } }
  a4: users { edges { node { id } } }
  a5: users { edges { node { id } } }
  a6: users { edges { node { id } } }
  a7: users { edges { node { id } } }
  a8: users { edges { node { id } } }
  a9: users { edges { node { id } } }
  a10: users { edges { node { id } } }
  a11: users { edges { node { id } } }
  a12: users { edges { node { id } } }
  a13: users { edges { node { id } } }
  a14: users { edges { node { id } } }
  a15: users { edges { node { id } } }
  a16: users { edges { node { id } } }
  a17: users { edges { node { id } } }
  a18: users { edges { node { id } } }
  a19: users { edges { node { id } } }
  a20: users { edges { node { id } } }
  a21: users { edges { node { id } } }
  a22: users { edges { node { id } } }
  a23: users { edges { node { id } } }
  a24: users { edges { node { id } } }
  a25: users { edges { node { id } } }
  a26: users { edges { node { id } } }
}` + "`" + `, true},
		{"unexposed_create", ` + "`" + `mutation UnexposedCreate {
  createUser(input: { name: "bad", email: "bad@example.com" }) { id }
}` + "`" + `, true},
		{"unexposed_delete", ` + "`" + `mutation UnexposedDelete {
  deleteUser(id: "00000000-0000-0000-0000-000000000001")
}` + "`" + `, true},
		{"relationship_fanout", ` + "`" + `query RelationshipFanout {
  users(page: { first: 100 }) {
    edges { node { posts { comments { body } } } }
  }
}` + "`" + `, true},
	}

	handler := graphql.Handler()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"query": tc.query})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var resp struct {
				Data   any              ` + "`" + `json:"data"` + "`" + `
				Errors []map[string]any ` + "`" + `json:"errors"` + "`" + `
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
			}
			if tc.wantError && len(resp.Errors) == 0 {
				t.Fatalf("expected GraphQL errors, got body %s", rec.Body.String())
			}
			if !tc.wantError && len(resp.Errors) != 0 {
				t.Fatalf("unexpected GraphQL errors: %v\n%s", resp.Errors, rec.Body.String())
			}
		})
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphql", "exported_safety_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
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
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "apiRoutes.API.RegisterRoutes(mux)")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `http.StripPrefix("/worker", workerRoutes.API)`)
	assertNoGoFileContains(t, out, "QueryOrder")
	writeExportedMonorepoServerBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedMonorepoServerBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apiRoutes "monorepo/services/api/routes"
	workerRoutes "monorepo/services/worker/routes"
)

func TestExportedMultiServiceServerMountsServiceLocalRoutes(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	mux := http.NewServeMux()
	apiRoutes.API.RegisterRoutes(mux)
	mux.Handle("/worker/", http.StripPrefix("/worker", workerRoutes.API))

	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, httptest.NewRequest(http.MethodGet, "/api/orders", nil))
	if apiRec.Code != http.StatusUnauthorized {
		t.Fatalf("api service status = %d body=%s", apiRec.Code, apiRec.Body.String())
	}

	workerRec := httptest.NewRecorder()
	mux.ServeHTTP(workerRec, httptest.NewRequest(http.MethodGet, "/worker/api/jobs", nil))
	if workerRec.Code != http.StatusUnauthorized {
		t.Fatalf("worker service status = %d body=%s", workerRec.Code, workerRec.Body.String())
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "cmd", "server", "exported_multiservice_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
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
	"strings"
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

func TestMigrationTableDDLMatchesDriver(t *testing.T) {
	cases := map[string]string{
		"sqlite":   "AUTOINCREMENT",
		"postgres": "SERIAL PRIMARY KEY",
		"pgsql":    "SERIAL PRIMARY KEY",
		"mysql":    "AUTO_INCREMENT",
	}
	for driver, want := range cases {
		if got := migrationsTableSQL(driver); !strings.Contains(got, want) {
			t.Fatalf("migrationsTableSQL(%q) = %q, want it to contain %q", driver, got, want)
		}
	}
	if got := migrationsTableSQL("mysql"); strings.Contains(got, "AUTOINCREMENT") {
		t.Fatalf("mysql migration table uses sqlite autoincrement: %q", got)
	}
}

func TestNormalizeSQLForMySQL(t *testing.T) {
	input := "CREATE TABLE \"events\" (\n" +
		"\t\"id\" UUID PRIMARY KEY,\n" +
		"\t\"payload\" JSONB NOT NULL,\n" +
		"\t\"body\" BYTEA,\n" +
		"\t\"created_at\" TIMESTAMPTZ NOT NULL DEFAULT NOW(),\n" +
		"\t\"nonce\" UUID DEFAULT gen_random_uuid()\n" +
		");\n" +
		"DROP TABLE IF EXISTS \"events\" CASCADE;"
	got := normalizeSQLForDriver(input, "mysql")
	bt := string(rune(96))
	for _, want := range []string{
		"CREATE TABLE " + bt + "events" + bt,
		bt + "id" + bt + " CHAR(36) PRIMARY KEY",
		bt + "payload" + bt + " JSON NOT NULL",
		bt + "body" + bt + " BLOB",
		bt + "created_at" + bt + " DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"DROP TABLE IF EXISTS " + bt + "events" + bt + ";",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("mysql normalized SQL missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "CASCADE") || strings.Contains(got, "JSONB") || strings.Contains(got, "TIMESTAMPTZ") || strings.Contains(got, string(rune(34))) {
		t.Fatalf("mysql normalized SQL retained unsupported syntax:\n%s", got)
	}
}

func TestNormalizeSQLForSQLite(t *testing.T) {
	input := "CREATE TABLE \"events\" (\"id\" UUID DEFAULT gen_random_uuid(), \"payload\" JSONB, \"created_at\" TIMESTAMPTZ DEFAULT NOW())"
	got := normalizeSQLForDriver(input, "sqlite")
	for _, want := range []string{"\"id\" TEXT", "\"payload\" TEXT", "\"created_at\" DATETIME DEFAULT CURRENT_TIMESTAMP"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sqlite normalized SQL missing %q:\n%s", want, got)
		}
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
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
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

func TestExportReportSeparatesManualReviewFromUnsupported(t *testing.T) {
	out := t.TempDir()
	reportPath := filepath.Join(out, "EXPORT_REPORT.md")
	ex := &exporter{
		project:    &generator.Project{Dir: "source-app"},
		outDir:     out,
		modulePath: "exported-app",
		result: &Result{
			ReportPath: reportPath,
			Findings: []Finding{{
				File:    "database/migrations",
				Rule:    "raw_sql_migration",
				Message: "migration seed_users contains raw SQL; exported statements need driver-specific review",
			}},
		},
	}
	if err := ex.writeReport("gorm"); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	assertFileContains(t, reportPath, "## Unsupported\n\nNo unsupported export findings.")
	assertFileContains(t, reportPath, "## Manual Review")
	assertFileContains(t, reportPath, "`database/migrations` `raw_sql_migration`")
	assertFileNotContains(t, reportPath, "## Omitted")
}

func TestExportReportListsUnsupportedBoundariesOnlyWhenUnsupported(t *testing.T) {
	out := t.TempDir()
	reportPath := filepath.Join(out, "EXPORT_REPORT.md")
	ex := &exporter{
		project:    &generator.Project{Dir: "source-app"},
		outDir:     out,
		modulePath: "exported-app",
		result: &Result{
			ReportPath: reportPath,
			Findings: []Finding{{
				File:    filepath.Join("database", "actions", "users"),
				Rule:    "action_export_unsupported_signature",
				Message: "unsupported action signature",
			}},
		},
	}
	if err := ex.writeReport("gorm"); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	assertFileContains(t, reportPath, "## Unsupported")
	assertFileContains(t, reportPath, "`database/actions/users` `action_export_unsupported_signature` - unsupported action signature")
	assertFileNotContains(t, reportPath, "No unsupported export findings.")
	assertFileNotContains(t, reportPath, "## Manual Review")
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
