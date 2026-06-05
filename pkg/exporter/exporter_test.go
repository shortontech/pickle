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
	assertFileContains(t, filepath.Join(out, "app", "models", "db.go"), "func WithTransaction(fn func(tx *Tx) error) error")
	assertFileContains(t, filepath.Join(out, "app", "models", "db.go"), "func ApplyLockTimeout(db *gorm.DB, d time.Duration) error")
	assertFileContains(t, filepath.Join(out, "app", "models", "db.go"), "func OrderClause(column, direction string) string")
	assertFileContains(t, filepath.Join(out, "app", "models", "db.go"), "type LockOutsideTransactionError struct")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "CREATE TABLE")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "email_encrypted")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "password_hash_encrypted")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.down.sql"), "DROP TABLE")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "CREATE INDEX")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260228100000_create_user_post_stats_view.up.sql"), "CREATE VIEW")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260228100000_create_user_post_stats_view.up.sql"), `"u"."email_encrypted" AS "email"`)
	assertFileNotContains(t, filepath.Join(out, "database", "migrations", "20260228100000_create_user_post_stats_view.up.sql"), `"u"."email",`)
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Exported")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone JWT, OAuth client-credentials, and session auth drivers")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone RBAC and GraphQL policy state support with changelog tables")
	assertCleanExportReport(t, out)
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func Env(key, fallback string) string")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "type ConnectionConfig struct")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func OpenGORM(conn ConnectionConfig) *gorm.DB")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func sanitizedDatabaseStartupError")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), `log.Fatal(sanitizedDatabaseStartupError("config"))`)
	assertFileNotContains(t, filepath.Join(out, "config", "support.go"), "failed to open database: %v")
	assertFileNotContains(t, filepath.Join(out, "config", "support.go"), "failed to initialize database: %v")
	assertFileNotContains(t, filepath.Join(out, "config", "support.go"), "log.Fatal(err)")
	assertFileContains(t, filepath.Join(out, "config", "app.go"), "func app() AppConfig")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func BuiltinCommands() []Command")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func HTTPHandler() http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "ReadHeaderTimeout: 10 * time.Second")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "ReadTimeout:       30 * time.Second")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "WriteTimeout:      60 * time.Second")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "IdleTimeout:       120 * time.Second")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "MaxHeaderBytes:    1 << 20")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func commandFailureMessage")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func commandStartupFailureMessage")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func serverFailureMessage")
	assertFileNotContains(t, filepath.Join(out, "app", "commands", "support.go"), "log.Fatal(err)")
	assertFileNotContains(t, filepath.Join(out, "app", "commands", "support.go"), "failed to unwrap database handle")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "support.go"), "func (r *Runner) Migrate(entries []MigrationEntry) error")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "crypto/hmac")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "ErrInvalidToken")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "maxJWTTokenBytes")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "validJWTShape")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "maxJWTExpirySeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "boundedPositiveSeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "oauth.NewDriver")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "session.NewDriver")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "func DefaultAuthMiddleware")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "func ActiveDriverName")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "const maxAuthorizationHeaderBytes = 12 << 10")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "func (d *Driver) TokenEndpoint")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "maxOAuthTokenExpirySeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "maxOAuthBearerTokenBytes")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "maxOAuthAuthorizationHeaderBytes")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "boundedPositiveSeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func CSRF")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "len(parts[0]) != 64 || len(parts[1]) != 64")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func validSessionID")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func validCookieName")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "maxSessionTTLSeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "boundedPositiveSeconds")
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
	assertNoGoFileContains(t, out, "RegisterPickleEndpoints")
	assertNoGoFileContains(t, out, "/pickle/config/reload")
	assertFileContains(t, filepath.Join(out, "go.sum"), "gorm.io/gorm")
	writeExportedAuthBehaviorTest(t, out)
	writeExportedSessionCSRFBehaviorTest(t, out)
	writeExportedConfigBehaviorTest(t, out)
	writeExportedModelDBBehaviorTest(t, out)
	writeExportedActionAuditBehaviorTest(t, out)
	writeExportedMigrationBehaviorTest(t, out)
	writeExportedCommandAppBehaviorTest(t, out)
	writeExportedRequestBindingBehaviorTest(t, out)
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

func TestDatabaseStartupFatalMessagesAreSanitized(t *testing.T) {
	for _, operation := range []string{"open", "initialize", "other"} {
		msg := sanitizedDatabaseStartupError(operation)
		if strings.Contains(msg, "password") || strings.Contains(msg, "secret") || strings.Contains(msg, "swordfish") {
			t.Fatalf("startup error for %q leaked detail: %s", operation, msg)
		}
		if !strings.Contains(msg, "database") {
			t.Fatalf("startup error for %q missing database context: %s", operation, msg)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "config", "exported_config_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedModelDBBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestApplyLockTimeoutIsDriverAware(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDBWithDriver(db, "sqlite")

	if sql, arg, ok := lockTimeoutStatement(time.Second); ok || sql != "" || arg != nil {
		t.Fatalf("sqlite lock timeout statement = (%q, %#v, %v), want no-op", sql, arg, ok)
	}
	if err := ApplyLockTimeout(db, time.Second); err != nil {
		t.Fatalf("sqlite ApplyLockTimeout should no-op, got %v", err)
	}

	SetDBWithDriver(db, "postgres")
	sql, arg, ok := lockTimeoutStatement(1500*time.Millisecond)
	if !ok {
		t.Fatal("postgres lock timeout statement should be enabled")
	}
	if sql != "SET LOCAL lock_timeout = ?" || arg != "1500ms" {
		t.Fatalf("postgres lock timeout statement = (%q, %#v), want SET LOCAL/1500ms", sql, arg)
	}
}

func TestOrderClauseRejectsUnsafeInput(t *testing.T) {
	if got := OrderClause("created_at", " desc "); got != "created_at DESC" {
		t.Fatalf("OrderClause safe result = %q, want created_at DESC", got)
	}
	assertPanics(t, func() { OrderClause("created_at; DROP TABLE users", "ASC") })
	assertPanics(t, func() { OrderClause("created_at", "DESC; DROP TABLE users") })
	assertPanics(t, func() { OrderClause("", "ASC") })
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_db_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedAuthBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package auth_test

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
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
		` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE oauth_tokens (token TEXT PRIMARY KEY, client_id TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
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
	assertPanicsWith(t, "JWT_SECRET is required", func() {
		jwt.NewDriver(func(key, fallback string) string {
			if key == "JWT_SECRET" {
				return ""
			}
			return fallback
		}, db, "sqlite")
	})
	assertPanicsWith(t, "must be at least 48 bytes for HS384", func() {
		jwt.NewDriver(func(key, fallback string) string {
			switch key {
			case "JWT_SECRET":
				return "0123456789abcdef0123456789abcdef"
			case "JWT_ALGORITHM":
				return "HS384"
			default:
				return fallback
			}
		}, db, "sqlite")
	})
	auth.Init(env, db)

	hugeAuthHeader := strings.Repeat("x", 13<<10)
	hugeAuthReq, _ := http.NewRequest("GET", "/", nil)
	hugeAuthReq.Header.Set("Authorization", hugeAuthHeader)
	if _, err := auth.Authenticate(hugeAuthReq); err == nil {
		t.Fatal("oversized Authorization header should fail")
	} else if strings.Contains(err.Error(), strings.Repeat("x", 128)) {
		t.Fatalf("oversized Authorization error leaked header value: %v", err)
	}

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
	infoClaims, ok := info.Claims.(jwt.Claims)
	if !ok {
		t.Fatalf("jwt auth claims type = %T", info.Claims)
	}
	if infoClaims.Subject != "user-1" || infoClaims.Role != "admin" || infoClaims.JTI == "" {
		t.Fatalf("jwt auth claims = %#v", infoClaims)
	}
	ctxWithClaims := httpx.NewContext(req)
	ctxWithClaims.SetAuth(info)
	contextClaims, ok := ctxWithClaims.Auth().Claims.(jwt.Claims)
	if !ok {
		t.Fatalf("ctx auth claims type = %T", ctxWithClaims.Auth().Claims)
	}
	if contextClaims.JTI != infoClaims.JTI {
		t.Fatalf("ctx auth claims JTI = %q, want %q", contextClaims.JTI, infoClaims.JTI)
	}
	claims, err := jwtDriver.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate jwt: %v", err)
	}
	_, err = jwtDriver.ValidateToken("not-a-jwt")
	assertInvalidJWT(t, err)
	_, err = jwtDriver.ValidateToken(strings.Repeat("a", 9000))
	assertInvalidJWT(t, err)
	_, err = jwtDriver.ValidateToken(strings.Repeat("a", 5000) + ".b.c")
	assertInvalidJWT(t, err)
	_, err = jwtDriver.ValidateToken("a..c")
	assertInvalidJWT(t, err)
	_, err = jwtDriver.ValidateToken("a.b.c.d")
	assertInvalidJWT(t, err)
	expiredToken, err := jwtDriver.SignToken(jwt.Claims{Subject: "user-expired", Role: "admin", ExpiresAt: time.Now().Add(-time.Hour).Unix()})
	if err != nil {
		t.Fatalf("sign expired jwt: %v", err)
	}
	_, err = jwtDriver.ValidateToken(expiredToken)
	assertInvalidJWT(t, err)
	if err := jwtDriver.RevokeToken(claims.JTI); err != nil {
		t.Fatalf("revoke jwt: %v", err)
	}
	if _, err := jwtDriver.Authenticate(req); err == nil {
		t.Fatal("revoked jwt should fail validation")
	} else if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("revoked jwt error = %v, want ErrInvalidToken", err)
	}
	token, err = jwtDriver.SignToken(jwt.Claims{Subject: "user-1", Role: "admin"})
	if err != nil {
		t.Fatalf("sign jwt after revoke: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := jwtDriver.Authenticate(req); err != nil {
		t.Fatalf("new jwt should authenticate: %v", err)
	}
	if err := jwtDriver.RevokeAllForUser("user-1"); err != nil {
		t.Fatalf("revoke all jwt: %v", err)
	}
	if _, err := jwtDriver.Authenticate(req); err == nil {
		t.Fatal("user-wide revoked jwt should fail validation")
	} else if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("user-wide revoked jwt error = %v, want ErrInvalidToken", err)
	}
	token, err = jwtDriver.SignToken(jwt.Claims{Subject: "user-1", Role: "admin"})
	if err != nil {
		t.Fatalf("sign jwt after revoke all: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := db.Exec("DELETE FROM jwt_tokens"); err != nil {
		t.Fatal(err)
	}
	if _, err := jwtDriver.Authenticate(req); err == nil {
		t.Fatal("revoked jwt should fail allowlist validation")
	} else if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("missing allowlist jwt error = %v, want ErrInvalidToken", err)
	}
	cappedJWTDriver := jwt.NewDriver(func(key, fallback string) string {
		switch key {
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		case "JWT_EXPIRY":
			return strings.Repeat("9", 80)
		default:
			return fallback
		}
	}, db, "sqlite")
	cappedJWT, err := cappedJWTDriver.SignToken(jwt.Claims{Subject: "user-capped", Role: "admin"})
	if err != nil {
		t.Fatalf("sign capped jwt: %v", err)
	}
	cappedJWTClaims, err := cappedJWTDriver.ValidateToken(cappedJWT)
	if err != nil {
		t.Fatalf("validate capped jwt: %v", err)
	}
	if cappedJWTClaims.ExpiresAt-cappedJWTClaims.IssuedAt != 365*24*60*60 {
		t.Fatalf("capped jwt expiry seconds = %d, want %d", cappedJWTClaims.ExpiresAt-cappedJWTClaims.IssuedAt, 365*24*60*60)
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
	tokenReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.SetBasicAuth("client-1", "secret-1")
	tokenResp := oauthDriver.TokenEndpoint(httpx.NewContext(tokenReq))
	if tokenResp.StatusCode != http.StatusOK {
		t.Fatalf("oauth token endpoint status = %d body = %#v", tokenResp.StatusCode, tokenResp.Body)
	}
	if tokenResp.Headers["Cache-Control"] != "no-store" || tokenResp.Headers["Pragma"] != "no-cache" {
		t.Fatalf("oauth token cache headers = %#v", tokenResp.Headers)
	}
	tokenBody, ok := tokenResp.Body.(map[string]any)
	if !ok {
		t.Fatalf("oauth token body type = %T", tokenResp.Body)
	}
	issuedToken, ok := tokenBody["access_token"].(string)
	if !ok || issuedToken == "" {
		t.Fatalf("oauth access token body = %#v", tokenBody)
	}
	issuedInfo, err := oauthDriver.ValidateToken(issuedToken)
	if err != nil {
		t.Fatalf("validate issued oauth token: %v", err)
	}
	if issuedInfo.UserID != "client-1" || issuedInfo.Role != "client" {
		t.Fatalf("issued oauth auth info = %#v", issuedInfo)
	}
	if _, err := oauthDriver.ValidateToken(""); err == nil || err.Error() != "oauth: invalid token" {
		t.Fatalf("empty oauth bearer token error = %v, want sanitized invalid token", err)
	}
	if _, err := oauthDriver.ValidateToken(strings.Repeat("x", 9<<10)); err == nil || err.Error() != "oauth: invalid token" || strings.Contains(err.Error(), strings.Repeat("x", 128)) {
		t.Fatalf("oversized oauth bearer token error = %v, want sanitized invalid token", err)
	}
	tokenReqWithCharset, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	tokenReqWithCharset.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	tokenReqWithCharset.SetBasicAuth("client-1", "secret-1")
	tokenRespWithCharset := oauthDriver.TokenEndpoint(httpx.NewContext(tokenReqWithCharset))
	if tokenRespWithCharset.StatusCode != http.StatusOK {
		t.Fatalf("oauth token endpoint charset status = %d body = %#v", tokenRespWithCharset.StatusCode, tokenRespWithCharset.Body)
	}
	badContentTypeReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	badContentTypeReq.Header.Set("Content-Type", "application/x-www-form-urlencodedevil")
	badContentTypeReq.SetBasicAuth("client-1", "secret-1")
	badContentTypeResp := oauthDriver.TokenEndpoint(httpx.NewContext(badContentTypeReq))
	if badContentTypeResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oauth bad content type status = %d body = %#v", badContentTypeResp.StatusCode, badContentTypeResp.Body)
	}
	badTokenReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	badTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badTokenReq.SetBasicAuth("client-1", "wrong")
	badTokenResp := oauthDriver.TokenEndpoint(httpx.NewContext(badTokenReq))
	if badTokenResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid oauth credentials status = %d body = %#v", badTokenResp.StatusCode, badTokenResp.Body)
	}
	largeAuthHeaderReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	largeAuthHeaderReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	largeAuthHeaderReq.Header.Set("Authorization", "Basic "+strings.Repeat("x", 13<<10))
	largeAuthHeaderResp := oauthDriver.TokenEndpoint(httpx.NewContext(largeAuthHeaderReq))
	if largeAuthHeaderResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("oversized oauth Authorization status = %d body = %#v", largeAuthHeaderResp.StatusCode, largeAuthHeaderResp.Body)
	}
	if strings.Contains(fmt.Sprint(largeAuthHeaderResp.Body), strings.Repeat("x", 128)) {
		t.Fatalf("oversized oauth Authorization response leaked header: %#v", largeAuthHeaderResp.Body)
	}
	largeTokenReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials&padding="+strings.Repeat("x", 9000)))
	largeTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	largeTokenReq.SetBasicAuth("client-1", "secret-1")
	largeTokenResp := oauthDriver.TokenEndpoint(httpx.NewContext(largeTokenReq))
	if largeTokenResp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized oauth request status = %d body = %#v", largeTokenResp.StatusCode, largeTokenResp.Body)
	}
	streamingTokenReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials&padding="+strings.Repeat("x", 9000)))
	streamingTokenReq.ContentLength = -1
	streamingTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	streamingTokenReq.SetBasicAuth("client-1", "secret-1")
	streamingTokenResp := oauthDriver.TokenEndpoint(httpx.NewContext(streamingTokenReq))
	if streamingTokenResp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("streaming oversized oauth request status = %d body = %#v", streamingTokenResp.StatusCode, streamingTokenResp.Body)
	}
	misconfigured := oauth.NewDriver(func(string, string) string { return "" }, db, "sqlite")
	misconfiguredReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	misconfiguredReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	misconfiguredReq.SetBasicAuth("", "")
	misconfiguredResp := misconfigured.TokenEndpoint(httpx.NewContext(misconfiguredReq))
	if misconfiguredResp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("misconfigured oauth status = %d body = %#v", misconfiguredResp.StatusCode, misconfiguredResp.Body)
	}
	cappedOAuthDriver := oauth.NewDriver(func(key, fallback string) string {
		switch key {
		case "OAUTH_CLIENT_ID":
			return "client-1"
		case "OAUTH_CLIENT_SECRET":
			return "secret-1"
		case "OAUTH_TOKEN_EXPIRY":
			return strings.Repeat("9", 80)
		default:
			return fallback
		}
	}, db, "sqlite")
	cappedTokenReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	cappedTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cappedTokenReq.SetBasicAuth("client-1", "secret-1")
	cappedTokenResp := cappedOAuthDriver.TokenEndpoint(httpx.NewContext(cappedTokenReq))
	if cappedTokenResp.StatusCode != http.StatusOK {
		t.Fatalf("capped oauth token endpoint status = %d body = %#v", cappedTokenResp.StatusCode, cappedTokenResp.Body)
	}
	cappedTokenBody, ok := cappedTokenResp.Body.(map[string]any)
	if !ok {
		t.Fatalf("capped oauth token body type = %T", cappedTokenResp.Body)
	}
	if cappedTokenBody["expires_in"] != 365*24*60*60 {
		t.Fatalf("capped oauth expires_in = %#v, want %d", cappedTokenBody["expires_in"], 365*24*60*60)
	}

	sessionDriver := auth.Driver("session").(*session.Driver)
	sessionID := "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)", sessionID, "user-2", "viewer", time.Now().Add(time.Hour), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	sessionReq, _ := http.NewRequest("GET", "/", nil)
	sessionReq.AddCookie(&http.Cookie{Name: sessionDriver.CookieName(), Value: sessionID})
	sessionInfo, err := sessionDriver.Authenticate(sessionReq)
	if err != nil {
		t.Fatalf("authenticate session: %v", err)
	}
	if sessionInfo.UserID != "user-2" || sessionInfo.Role != "viewer" {
		t.Fatalf("session auth info = %#v", sessionInfo)
	}
	sessionCtx := httpx.NewContext(sessionReq)
	if err := session.Put(sessionCtx, "onboarding_step", "3"); err != nil {
		t.Fatalf("session put string: %v", err)
	}
	step, err := session.Get(sessionCtx, "onboarding_step")
	if err != nil {
		t.Fatalf("session get string: %v", err)
	}
	if step != "3" {
		t.Fatalf("session onboarding_step = %q, want 3", step)
	}
	if err := session.Put(sessionCtx, "settings", map[string]any{"dark": true}); err != nil {
		t.Fatalf("session put object: %v", err)
	}
	settings, err := session.Get(sessionCtx, "settings")
	if err != nil {
		t.Fatalf("session get object: %v", err)
	}
	if settings != ` + "`" + `{"dark":true}` + "`" + ` {
		t.Fatalf("session settings = %q", settings)
	}
	destroyResp, err := session.Destroy(sessionCtx)
	if err != nil {
		t.Fatalf("session destroy: %v", err)
	}
	if destroyResp.StatusCode != 204 {
		t.Fatalf("destroy status = %d, want 204", destroyResp.StatusCode)
	}
	if len(destroyResp.Cookies) != 2 {
		t.Fatalf("destroy cookies = %d, want session and csrf", len(destroyResp.Cookies))
	}
	for _, cookie := range destroyResp.Cookies {
		if cookie.MaxAge != -1 {
			t.Fatalf("destroy cookie %#v should expire browser cookie", cookie)
		}
	}
	if _, err := sessionDriver.Authenticate(sessionReq); err == nil {
		t.Fatal("destroyed session should fail authentication")
	}
	oversizedSessionReq, _ := http.NewRequest("GET", "/", nil)
	oversizedSessionReq.AddCookie(&http.Cookie{Name: sessionDriver.CookieName(), Value: strings.Repeat("x", 256)})
	if _, err := sessionDriver.Authenticate(oversizedSessionReq); err == nil || strings.Contains(err.Error(), strings.Repeat("x", 64)) {
		t.Fatalf("oversized session cookie error = %v, want sanitized invalid session", err)
	}
	expiredSessionID := "22222222-2222-4222-8222-222222222222"
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, payload, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", expiredSessionID, "user-2", "viewer", ` + "`" + `{"stale":"secret"}` + "`" + `, time.Now().Add(-time.Hour), time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	expiredReq, _ := http.NewRequest("GET", "/", nil)
	expiredReq.AddCookie(&http.Cookie{Name: sessionDriver.CookieName(), Value: expiredSessionID})
	expiredCtx := httpx.NewContext(expiredReq)
	if _, err := session.Get(expiredCtx, "stale"); err == nil {
		t.Fatal("expired session get should fail")
	}
	if err := session.Put(expiredCtx, "stale", "updated"); err == nil {
		t.Fatal("expired session put should fail")
	}
	cappedSessionDriver := session.NewDriver(func(key, fallback string) string {
		switch key {
		case "SESSION_SECRET":
			return "session-secret"
		case "SESSION_TTL":
			return strings.Repeat("9", 80)
		default:
			return fallback
		}
	}, db, "sqlite")
	if cappedSessionDriver.TTL() != 365*24*60*60 {
		t.Fatalf("capped session TTL = %d, want %d", cappedSessionDriver.TTL(), 365*24*60*60)
	}

	badEnv := func(key, fallback string) string {
		if key == "AUTH_DRIVER" {
			return "bogus"
		}
		return env(key, fallback)
	}
	assertSanitizedAuthInitPanic(t, "bogus", func() {
		auth.Init(badEnv, db)
	})
	if _, err := auth.TryActiveDriver(); err == nil {
		t.Fatal("TryActiveDriver should reject unknown AUTH_DRIVER")
	}
	if _, err := auth.Authenticate(req); err == nil {
		t.Fatal("Authenticate should return an error for unknown AUTH_DRIVER")
	}
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)
	resp := auth.DefaultAuthMiddleware(httpx.NewContext(req), func() httpx.Response {
		t.Fatal("middleware should not call next for unknown AUTH_DRIVER")
		return httpx.Response{}
	})
	if resp.Status != 401 {
		t.Fatalf("middleware status = %d, want 401", resp.Status)
	}
	body, ok := resp.Body.(map[string]string)
	if !ok {
		t.Fatalf("middleware body type = %T", resp.Body)
	}
	if body["error"] != "unauthorized" || strings.Contains(body["error"], "bogus") {
		t.Fatalf("middleware error body = %#v, want sanitized unauthorized", body)
	}
	if strings.Contains(logs.String(), "bogus") {
		t.Fatalf("middleware auth log leaked driver detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "auth middleware failed") {
		t.Fatalf("middleware auth log missing sanitized marker: %s", logs.String())
	}

	auth.Init(env, db)
	token, err = jwtDriver.SignToken(jwt.Claims{Subject: "user-4", Role: "editor"})
	if err != nil {
		t.Fatalf("sign jwt for middleware: %v", err)
	}
	okReq, _ := http.NewRequest("GET", "/", nil)
	okReq.Header.Set("Authorization", "Bearer "+token)
	okCtx := httpx.NewContext(okReq)
	okResp := auth.DefaultAuthMiddleware(okCtx, func() httpx.Response {
		if okCtx.Auth() == nil || okCtx.Auth().UserID != "user-4" || okCtx.Auth().Role != "editor" {
			t.Fatalf("middleware auth info = %#v", okCtx.Auth())
		}
		return okCtx.NoContent()
	})
	if okResp.StatusCode != http.StatusNoContent {
		t.Fatalf("successful middleware status = %d, want %d", okResp.StatusCode, http.StatusNoContent)
	}
}

func TestExportedAuthInitOnlyRequiresActiveDriverConfig(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}

	auth.Init(func(key, fallback string) string {
		values := map[string]string{
			"AUTH_DRIVER": "session",
			"DB_CONNECTION": "sqlite",
			"SESSION_SECRET": "session-secret",
		}
		if value, ok := values[key]; ok {
			return value
		}
		return fallback
	}, db)

	if auth.ActiveDriverName() != "session" {
		t.Fatalf("active driver = %q, want session", auth.ActiveDriverName())
	}
	if _, ok := auth.ActiveDriver().(*session.Driver); !ok {
		t.Fatalf("active driver type = %T, want session driver", auth.ActiveDriver())
	}

	ctx := httpx.NewContext(httptest.NewRequest(http.MethodPost, "/login", nil))
	cookies, err := session.Create(ctx, "user-3", "member")
	if err != nil {
		t.Fatalf("session create without JWT_SECRET: %v", err)
	}
	if cookies.Session == nil || cookies.CSRF == nil {
		t.Fatalf("session create cookies = %#v", cookies)
	}
	if _, err := auth.TryDriver("jwt"); err == nil || !strings.Contains(err.Error(), "JWT_SECRET is required") {
		t.Fatalf("TryDriver(jwt) error = %v, want JWT_SECRET configuration error", err)
	}
}

func TestExportedAuthInitSanitizesActiveDriverFailures(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("auth.Init should panic for misconfigured active jwt driver")
		}
		msg := fmt.Sprint(recovered)
		if msg != "auth: active driver initialization failed" {
			t.Fatalf("panic = %q, want sanitized active-driver failure", msg)
		}
		if strings.Contains(msg, "JWT_SECRET") || strings.Contains(msg, "required") {
			t.Fatalf("active-driver panic leaked config detail: %q", msg)
		}
	}()

	auth.Init(func(key, fallback string) string {
		switch key {
		case "AUTH_DRIVER":
			return "jwt"
		case "JWT_SECRET":
			return ""
		default:
			return fallback
		}
	}, db)
}

func assertSanitizedAuthInitPanic(t *testing.T, forbidden string, fn func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("auth.Init should panic for invalid active auth driver")
		}
		msg := fmt.Sprint(recovered)
		if msg != "auth: active driver initialization failed" {
			t.Fatalf("panic = %q, want sanitized active-driver failure", msg)
		}
		if forbidden != "" && strings.Contains(msg, forbidden) {
			t.Fatalf("active-driver panic leaked detail %q in %q", forbidden, msg)
		}
	}()
	fn()
}

func assertPanicsWith(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if !strings.Contains(fmt.Sprint(got), want) {
			t.Fatalf("panic = %q, want containing %q", got, want)
		}
	}()
	fn()
}

func assertInvalidJWT(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("jwt error = %v, want ErrInvalidToken", err)
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

func writeExportedRequestBindingBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package requests

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestExportedRequestBindingsRejectUnsafeBodies(t *testing.T) {
	validReq := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `)
	if req, bindErr := BindCreateUserRequest(validReq); bindErr != nil {
		t.Fatalf("valid bind error = %v", bindErr)
	} else if req.Name != "Ada" || req.Email != "ada@example.com" {
		t.Fatalf("valid request = %#v", req)
	}

	unknownField := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse","admin":true}` + "`" + `)
	if _, bindErr := BindCreateUserRequest(unknownField); bindErr == nil || bindErr.Status != http.StatusBadRequest {
		t.Fatalf("unknown-field bind error = %#v, want 400", bindErr)
	}

	trailingJSON := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"} {"name":"Grace"}` + "`" + `)
	if _, bindErr := BindCreateUserRequest(trailingJSON); bindErr == nil || bindErr.Status != http.StatusBadRequest {
		t.Fatalf("trailing JSON bind error = %#v, want 400", bindErr)
	}

	invalid := requestWithBody(` + "`" + `{"name":"Ada","email":"not-an-email","password":"short"}` + "`" + `)
	if _, bindErr := BindCreateUserRequest(invalid); bindErr == nil || bindErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("validation bind error = %#v, want 422", bindErr)
	}

	fallbackErr := formatValidationErrors(errors.New("database password is swordfish"))
	if fallbackErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("fallback validation status = %d, want 422", fallbackErr.Status)
	}
	if len(fallbackErr.Errors) != 1 || fallbackErr.Errors[0].Message != "validation failed" {
		t.Fatalf("fallback validation errors = %#v", fallbackErr.Errors)
	}
	if strings.Contains(fallbackErr.Error(), "swordfish") || strings.Contains(fallbackErr.Error(), "database password") {
		t.Fatalf("fallback validation error leaked raw detail: %q", fallbackErr.Error())
	}

	oversized := requestWithBody(strings.Repeat(" ", maxJSONRequestBodyBytes+1))
	if _, bindErr := BindCreateUserRequest(oversized); bindErr == nil || bindErr.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized bind error = %#v, want 413", bindErr)
	}

	streaming := requestWithBody(strings.Repeat(" ", maxJSONRequestBodyBytes+1))
	streaming.ContentLength = -1
	if _, bindErr := BindCreateUserRequest(streaming); bindErr == nil || bindErr.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("streaming oversized bind error = %#v, want 413", bindErr)
	}
}

func requestWithBody(body string) *http.Request {
	req, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "http", "requests", "exported_binding_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedSessionCSRFBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package session

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"basic-crud/internal/httpx"
)

func TestExportedSessionCSRFBoundary(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
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

	sessionID := "11111111-1111-4111-8111-111111111111"
	getCtx := httpx.NewContext(requestWithSession(http.MethodGet, sessionID))
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

	anonymousGetCtx := httpx.NewContext(httptest.NewRequest(http.MethodGet, "/", nil))
	anonymousGetResp := CSRF(anonymousGetCtx, func() httpx.Response {
		return anonymousGetCtx.NoContent()
	})
	if anonymousGetResp.StatusCode != http.StatusNoContent {
		t.Fatalf("anonymous GET status = %d", anonymousGetResp.StatusCode)
	}
	if cookie := findCookie(anonymousGetResp.Cookies, "csrf_token"); cookie != nil {
		t.Fatalf("anonymous safe request should not receive CSRF cookie: %#v", cookie)
	}

	oversizedSessionGetCtx := httpx.NewContext(requestWithSession(http.MethodGet, strings.Repeat("x", 256)))
	oversizedSessionGetResp := CSRF(oversizedSessionGetCtx, func() httpx.Response {
		return oversizedSessionGetCtx.NoContent()
	})
	if oversizedSessionGetResp.StatusCode != http.StatusNoContent {
		t.Fatalf("oversized session safe request status = %d", oversizedSessionGetResp.StatusCode)
	}
	if cookie := findCookie(oversizedSessionGetResp.Cookies, "csrf_token"); cookie != nil {
		t.Fatalf("oversized session cookie should not receive CSRF cookie: %#v", cookie)
	}

	postCtx := httpx.NewContext(requestWithSession(http.MethodPost, sessionID))
	missing := CSRF(postCtx, func() httpx.Response {
		t.Fatal("CSRF should block missing token")
		return httpx.Response{}
	})
	if missing.StatusCode != http.StatusForbidden {
		t.Fatalf("missing token status = %d", missing.StatusCode)
	}

	invalidReq := requestWithSession(http.MethodPost, sessionID)
	invalidReq.Header.Set("X-CSRF-TOKEN", "bogus.token")
	invalidCtx := httpx.NewContext(invalidReq)
	invalid := CSRF(invalidCtx, func() httpx.Response {
		t.Fatal("CSRF should block invalid token")
		return httpx.Response{}
	})
	if invalid.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid token status = %d", invalid.StatusCode)
	}

	oversizedReq := requestWithSession(http.MethodPost, sessionID)
	oversizedReq.Header.Set("X-CSRF-TOKEN", strings.Repeat("a", 4096)+"."+strings.Repeat("b", 4096))
	oversizedCtx := httpx.NewContext(oversizedReq)
	oversized := CSRF(oversizedCtx, func() httpx.Response {
		t.Fatal("CSRF should block oversized token")
		return httpx.Response{}
	})
	if oversized.StatusCode != http.StatusForbidden {
		t.Fatalf("oversized token status = %d", oversized.StatusCode)
	}

	anonymousPostReq := httptest.NewRequest(http.MethodPost, "/", nil)
	anonymousPostReq.Header.Set("X-CSRF-TOKEN", generateCSRFToken("", csrfConfig.secret))
	anonymousPostCtx := httpx.NewContext(anonymousPostReq)
	anonymousPost := CSRF(anonymousPostCtx, func() httpx.Response {
		t.Fatal("CSRF should block unsafe request without session")
		return httpx.Response{}
	})
	if anonymousPost.StatusCode != http.StatusForbidden {
		t.Fatalf("anonymous POST status = %d", anonymousPost.StatusCode)
	}

	validReq := requestWithSession(http.MethodPost, sessionID)
	validReq.Header.Set("X-CSRF-TOKEN", generateCSRFToken(sessionID, csrfConfig.secret))
	validCtx := httpx.NewContext(validReq)
	valid := CSRF(validCtx, func() httpx.Response {
		return validCtx.NoContent()
	})
	if valid.StatusCode != http.StatusNoContent {
		t.Fatalf("valid token status = %d", valid.StatusCode)
	}

	bearerReq := requestWithSession(http.MethodPost, sessionID)
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
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, db, "sqlite")

	ctx := httpx.NewContext(httptest.NewRequest(http.MethodPost, "/login", nil))
	cookies, err := Create(ctx, "user-1", "member")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if cookies.Session == nil {
		t.Fatal("session cookie should be returned")
	}
	if cookies.CSRF == nil {
		t.Fatal("csrf cookie should be returned")
	}
	resp := cookies.Apply(ctx.JSON(http.StatusCreated, map[string]string{"ok": "true"}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("applied response status = %d", resp.StatusCode)
	}
	if body, ok := resp.Body.(map[string]string); !ok || body["ok"] != "true" {
		t.Fatalf("applied response body = %#v", resp.Body)
	}
	if findCookie(resp.Cookies, sessionCookieName) == nil {
		t.Fatal("session cookie should be set")
	}
	if findCookie(resp.Cookies, csrfConfig.cookieName) == nil {
		t.Fatal("csrf cookie should be set")
	}
}

func TestExportedSessionRejectsInvalidCookieNames(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, name := range []string{"sid", "sid.v1", "sid_v1"} {
		if !validCookieName(name) {
			t.Fatalf("validCookieName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "bad cookie", "bad;cookie", "bad\ncookie", strings.Repeat("x", 65)} {
		if validCookieName(name) {
			t.Fatalf("validCookieName(%q) = true, want false", name)
		}
	}

	assertSessionDriverPanic(t, "invalid SESSION_COOKIE", func() {
		NewDriver(func(key, fallback string) string {
			if key == "SESSION_COOKIE" {
				return "bad cookie"
			}
			return fallback
		}, db, "sqlite")
	})
	assertSessionDriverPanic(t, "invalid CSRF_COOKIE", func() {
		NewDriver(func(key, fallback string) string {
			if key == "CSRF_COOKIE" {
				return strings.Repeat("x", 65)
			}
			return fallback
		}, db, "sqlite")
	})
}

func assertSessionDriverPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if !strings.Contains(recovered.(string), want) {
			t.Fatalf("panic = %v, want %q", recovered, want)
		}
	}()
	fn()
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
	ctx := httpx.NewContext(requestWithSession(http.MethodPost, "11111111-1111-4111-8111-111111111111"))
	resp := CSRF(ctx, func() httpx.Response {
		t.Fatal("CSRF should not call next when secret is missing")
		return httpx.Response{}
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing secret status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestExportedSessionPayloadErrorsAreSanitized(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, db, "sqlite")

	badPayloadSessionID := "33333333-3333-4333-8333-333333333333"
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, payload, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", badPayloadSessionID, "user-1", "member", "{secret", time.Now().Add(time.Hour), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	badCtx := httpx.NewContext(requestWithSession(http.MethodGet, badPayloadSessionID))
	if _, err := Get(badCtx, "secret"); err == nil || err.Error() != "session: invalid session payload" || strings.Contains(err.Error(), "{secret") {
		t.Fatalf("Get invalid payload error = %v, want sanitized invalid payload", err)
	}
	if err := Put(badCtx, "secret", "value"); err == nil || err.Error() != "session: invalid session payload" || strings.Contains(err.Error(), "{secret") {
		t.Fatalf("Put invalid payload error = %v, want sanitized invalid payload", err)
	}

	missingCtx := httpx.NewContext(requestWithSession(http.MethodGet, "44444444-4444-4444-8444-444444444444"))
	if _, err := Get(missingCtx, "secret"); err == nil || err.Error() != "session: invalid or expired session" || strings.Contains(err.Error(), "sql:") {
		t.Fatalf("Get missing session error = %v, want sanitized invalid session", err)
	}
	if err := Put(missingCtx, "secret", "value"); err == nil || err.Error() != "session: invalid or expired session" || strings.Contains(err.Error(), "sql:") {
		t.Fatalf("Put missing session error = %v, want sanitized invalid session", err)
	}
}

func TestExportedSessionCreateDestroyDatabaseErrorsAreSanitized(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(` + "`" + `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT NOT NULL, payload TEXT, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, db, "sqlite")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := httpx.NewContext(requestWithSession(http.MethodPost, "11111111-1111-4111-8111-111111111111"))
	if _, err := Create(ctx, "user-1", "member"); err == nil || err.Error() != "session: database error" || strings.Contains(err.Error(), "sql:") {
		t.Fatalf("Create closed DB error = %v, want sanitized database error", err)
	}
	if _, err := Destroy(ctx); err == nil || err.Error() != "session: database error" || strings.Contains(err.Error(), "sql:") {
		t.Fatalf("Destroy closed DB error = %v, want sanitized database error", err)
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
	"bytes"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
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

	var performed, denied, failed int
	var deniedReason, failedError string
	models.OnAuditPerformed = func(ctx *httpx.Context, action, model string, resourceID any, extra string) {
		if action == "Ban" && model == "User" {
			performed++
		}
	}
	models.OnAuditDenied = func(ctx *httpx.Context, action, model string, resourceID any, extra string) {
		if action == "Ban" && model == "User" {
			denied++
			deniedReason = extra
		}
	}
	models.OnAuditFailed = func(ctx *httpx.Context, action, model string, resourceID any, extra string) {
		if action == "Fail" && model == "User" {
			failed++
			failedError = extra
		}
	}
	defer func() {
		models.OnAuditPerformed = nil
		models.OnAuditDenied = nil
		models.OnAuditFailed = nil
	}()

	user := &models.User{ID: userID, Name: "before"}
	if err := user.Ban(ctx, models.BanAction{Reason: "banned"}); err != nil {
		t.Fatalf("ban: %v", err)
	}
	if performed != 1 {
		t.Fatalf("performed audit hook count = %d, want 1", performed)
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

	badAuditCtx := httpx.NewContext(req)
	badAuditCtx.SetAuth(&httpx.AuthInfo{UserID: "not-a-uuid", Role: "admin"})
	if err := user.Ban(badAuditCtx, models.BanAction{Reason: "audit-fail"}); err == nil || !strings.Contains(err.Error(), "audit user id") {
		t.Fatalf("ban with invalid audit user id error = %v, want audit user id error", err)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows after failed audit persistence = %d, want 1", auditRows)
	}
	var persistedName string
	if err := db.Raw("SELECT name FROM users WHERE id = ?", userID.String()).Scan(&persistedName).Error; err != nil {
		t.Fatal(err)
	}
	if persistedName != "banned" {
		t.Fatalf("user name after failed audit persistence = %q, want banned", persistedName)
	}

	deniedCtx := httpx.NewContext(req)
	deniedCtx.SetAuth(&httpx.AuthInfo{UserID: uuid.New().String(), Role: "viewer"})
	if err := user.Ban(deniedCtx, models.BanAction{Reason: "denied"}); !errors.Is(err, models.ErrUnauthorized) {
		t.Fatalf("denied ban error = %v, want ErrUnauthorized", err)
	}
	if denied != 1 || deniedReason != "gate denied" {
		t.Fatalf("denied audit hook count/reason = %d/%q, want 1/gate denied", denied, deniedReason)
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
	if failed != 1 || failedError != "action failed" {
		t.Fatalf("failed audit hook count/error = %d/%q, want 1/action failed", failed, failedError)
	}
	if strings.Contains(failedError, "boom") {
		t.Fatalf("failed audit hook leaked raw error: %q", failedError)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows after failed action = %d, want 1", auditRows)
	}

	models.OnAuditFailed = nil
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)
	models.OnAuditFailed = func(ctx *httpx.Context, action, model string, resourceID any, extra string) {
		failedError = extra
	}
	models.AuditFailed(ctx, "Fail", "User", user.ID, errors.New("database password is swordfish"))
	if failedError != "action failed" || strings.Contains(failedError, "swordfish") || strings.Contains(failedError, "password") {
		t.Fatalf("custom audit failed hook leaked detail: %q", failedError)
	}
	models.OnAuditFailed = nil
	models.AuditFailed(ctx, "Fail", "User", user.ID, errors.New("database password is swordfish"))
	if strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "password") {
		t.Fatalf("default audit failed log leaked detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "audit.failed") || !strings.Contains(logs.String(), "action failed") {
		t.Fatalf("default audit failed log missing sanitized marker: %s", logs.String())
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
	if err := db.Raw("SELECT * FROM user_post_stats LIMIT 0").Error; err != nil {
		t.Fatalf("query user_post_stats view: %v", err)
	}
}

func TestExportedMigrationFailuresAreAtomic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := migrations.NewRunner(db, "sqlite")
	failingMigrate := migrations.MigrationEntry{
		ID:       "99990101000000_atomic_failure",
		UpFile:   "99990101000000_atomic_failure.up.sql",
		DownFile: "99990101000000_atomic_failure.down.sql",
	}
	if err := runner.Migrate([]migrations.MigrationEntry{failingMigrate}); err == nil {
		t.Fatal("expected failing migration")
	}
	assertSQLiteTableMissing(t, db, "atomic_failure")
	assertMigrationRowCount(t, db, failingMigrate.ID, 0)

	successThenFailingRollback := migrations.MigrationEntry{
		ID:       "99990101000001_atomic_rollback",
		UpFile:   "99990101000001_atomic_rollback.up.sql",
		DownFile: "99990101000001_atomic_rollback.down.sql",
	}
	if err := runner.Migrate([]migrations.MigrationEntry{successThenFailingRollback}); err != nil {
		t.Fatalf("setup migration: %v", err)
	}
	assertSQLiteTableExists(t, db, "atomic_rollback")
	assertMigrationRowCount(t, db, successThenFailingRollback.ID, 1)
	if err := runner.Rollback([]migrations.MigrationEntry{successThenFailingRollback}); err == nil {
		t.Fatal("expected failing rollback")
	}
	assertSQLiteTableExists(t, db, "atomic_rollback")
	assertMigrationRowCount(t, db, successThenFailingRollback.ID, 1)
}

func assertSQLiteTableExists(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("table %s exists = %d, want 1", table, count)
	}
}

func assertSQLiteTableMissing(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("table %s exists = %d, want 0", table, count)
	}
}

func assertMigrationRowCount(t *testing.T, db *gorm.DB, id string, want int64) {
	t.Helper()
	var count int64
	if err := db.Table("migrations").Where("migration = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("migration rows for %s = %d, want %d", id, count, want)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "migrations", "exported_migration_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	internalTestSrc := `package migrations

import "testing"

func TestExportedSplitSQLStatementsPreservesQuotedSemicolons(t *testing.T) {
	sql := "INSERT INTO logs (message) VALUES ('hello; world');\n" +
		"INSERT INTO logs (message) VALUES ('it''s; fine');\n" +
		"CREATE TABLE \"semi;colon\" (id TEXT);\n" +
		"-- comment with ; semicolon\n" +
		"INSERT INTO logs (message) VALUES ('after line comment');\n" +
		"/* block comment ; still comment */\n" +
		"INSERT INTO logs (message) VALUES ('after block comment');"
	statements := splitSQLStatements(sql)
	if len(statements) != 5 {
		t.Fatalf("statements = %d, want 5: %#v", len(statements), statements)
	}
	want := []string{
		"INSERT INTO logs (message) VALUES ('hello; world')",
		"INSERT INTO logs (message) VALUES ('it''s; fine')",
		"CREATE TABLE \"semi;colon\" (id TEXT)",
		"-- comment with ; semicolon\nINSERT INTO logs (message) VALUES ('after line comment')",
		"/* block comment ; still comment */\nINSERT INTO logs (message) VALUES ('after block comment')",
	}
	for i := range want {
		if statements[i] != want[i] {
			t.Fatalf("statement %d = %q, want %q", i, statements[i], want[i])
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "migrations", "exported_migration_internal_test.go"), []byte(internalTestSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"99990101000000_atomic_failure.up.sql":    "CREATE TABLE atomic_failure (id TEXT PRIMARY KEY);\nSELECT * FROM definitely_missing_table;\n",
		"99990101000000_atomic_failure.down.sql":  "DROP TABLE IF EXISTS atomic_failure;\n",
		"99990101000001_atomic_rollback.up.sql":   "CREATE TABLE atomic_rollback (id TEXT PRIMARY KEY);\n",
		"99990101000001_atomic_rollback.down.sql": "DROP TABLE atomic_rollback;\nSELECT * FROM definitely_missing_table;\n",
	} {
		if err := os.WriteFile(filepath.Join(out, "database", "migrations", name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeExportedCommandAppBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package commands_test

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"basic-crud/app/commands"
)

func TestExportedCommandAppRunsMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.sqlite")
	t.Setenv("DB_CONNECTION", "sqlite")
	t.Setenv("DB_DATABASE", dbPath)
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("APP_ENCRYPTION_KEY", "12345678901234567890123456789012")

	commands.NewApp().Run([]string{"migrate"})
	commands.NewApp().Run([]string{"migrate:status"})

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var migrations int
	if err := db.QueryRow("SELECT COUNT(*) FROM migrations").Scan(&migrations); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrations == 0 {
		t.Fatal("migrate command did not record any migrations")
	}
	var roles int
	if err := db.QueryRow("SELECT COUNT(*) FROM roles").Scan(&roles); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if roles != 3 {
		t.Fatalf("roles after migrate command = %d, want 3", roles)
	}
	var exposures int
	if err := db.QueryRow("SELECT COUNT(*) FROM graphql_exposures WHERE model = 'users' AND operation = 'list'").Scan(&exposures); err != nil {
		t.Fatalf("count graphql exposures: %v", err)
	}
	if exposures != 1 {
		t.Fatalf("graphql user list exposures = %d, want 1", exposures)
	}

	commands.NewApp().Run([]string{"migrate:rollback"})
	migrations = countRows(t, db, "migrations")
	if migrations != 0 {
		t.Fatalf("migrations after rollback command = %d, want 0", migrations)
	}

	commands.NewApp().Run([]string{"migrate:fresh"})
	migrations = countRows(t, db, "migrations")
	if migrations == 0 {
		t.Fatal("fresh command did not re-record migrations")
	}
	roles = countRows(t, db, "roles")
	if roles != 3 {
		t.Fatalf("roles after fresh command = %d, want 3", roles)
	}
	exposures = countWhere(t, db, "graphql_exposures", "model = 'users' AND operation = 'list'")
	if exposures != 1 {
		t.Fatalf("graphql user list exposures after fresh = %d, want 1", exposures)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewBufferString(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	commands.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Ada") {
		t.Fatalf("create user response = %s, want Ada", rec.Body.String())
	}

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/pickle/health"},
		{http.MethodPost, "/pickle/config/reload"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		commands.HTTPHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404; body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	return countWhere(t, db, table, "1 = 1")
}

func countWhere(t *testing.T, db *sql.DB, table, where string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE " + where).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "commands", "exported_command_app_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	securityTestSrc := `package commands

import (
	"strings"
	"testing"
)

func TestExportedCommandFatalMessagesAreSanitized(t *testing.T) {
	for _, msg := range []string{
		commandFailureMessage("migrate"),
		commandStartupFailureMessage("database"),
		serverFailureMessage(assertSecretError("listen tcp :8080: password=swordfish")),
	} {
		if strings.Contains(msg, "swordfish") || strings.Contains(msg, "password") || strings.Contains(msg, "listen tcp") {
			t.Fatalf("fatal message leaked detail: %s", msg)
		}
		if !strings.Contains(msg, "failed") {
			t.Fatalf("fatal message = %q, want failure context", msg)
		}
	}
	if msg := unknownCommandMessage(); msg != "unknown command" {
		t.Fatalf("unknown command message = %q, want sanitized fixed message", msg)
	}
	if strings.Contains(unknownCommandMessage(), "token=secret") || strings.Contains(unknownCommandMessage(), "password") {
		t.Fatalf("unknown command message leaked detail: %s", unknownCommandMessage())
	}
}

type assertSecretError string

func (e assertSecretError) Error() string { return string(e) }
`
	if err := os.WriteFile(filepath.Join(out, "app", "commands", "exported_command_security_test.go"), []byte(securityTestSrc), 0o644); err != nil {
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

func TestExportedPolicyMigrateRollsBackSeedFailures(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		` + "`" + `CREATE TABLE roles (id TEXT PRIMARY KEY, slug VARCHAR(50) NOT NULL UNIQUE, name VARCHAR(100) NOT NULL, manages BOOLEAN NOT NULL DEFAULT false, is_default BOOLEAN NOT NULL DEFAULT false, birth_policy VARCHAR(100) NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE role_actions (id TEXT PRIMARY KEY, role_slug VARCHAR(50) NOT NULL, action VARCHAR(100) NOT NULL CHECK(action <> 'users.create'), created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, UNIQUE(role_slug, action))` + "`" + `,
		` + "`" + `CREATE TABLE role_user (user_id TEXT NOT NULL, role_id TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY(user_id, role_id))` + "`" + `,
		` + "`" + `CREATE TABLE rbac_changelog (id VARCHAR(255) PRIMARY KEY, batch INTEGER NOT NULL, state VARCHAR(20) NOT NULL, error TEXT, started_at DATETIME, completed_at DATETIME)` + "`" + `,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(db, "sqlite"); err == nil {
		t.Fatal("expected policy migrate to fail on constrained role action")
	}
	for _, table := range []string{"roles", "role_actions", "rbac_changelog"} {
		var rows int64
		if err := db.Table(table).Count(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("%s rows after failed migrate = %d, want 0", table, rows)
		}
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
	"bytes"
	"errors"
	"log"
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
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	var reported error
	router := httpx.Routes(func(r *httpx.Router) {
		r.OnError(func(ctx *httpx.Context, err error) {
			reported = err
			if ctx == nil || ctx.Param("id") != "123" {
				t.Fatalf("reported context = %#v", ctx)
			}
		})
		r.Get("/panic/:id", func(ctx *httpx.Context) httpx.Response {
			panic("database password is swordfish")
		})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic/123", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("panic Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("panic X-Content-Type-Options = %q, want nosniff", got)
	}
	if strings.Contains(rec.Body.String(), "swordfish") || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("panic response leaked detail: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Fatalf("panic response missing sanitized error: %s", rec.Body.String())
	}
	if reported == nil || reported.Error() != "panic recovered" {
		t.Fatalf("reported error = %v", reported)
	}
	if strings.Contains(reported.Error(), "swordfish") || strings.Contains(reported.Error(), "password") {
		t.Fatalf("reported error leaked detail: %v", reported)
	}
	if strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "password") {
		t.Fatalf("panic log leaked detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "panic recovered") {
		t.Fatalf("panic log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedContextResourceHelpersPropagateOwner(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

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

	failing := &resourceQuery{err: errors.New("database password is swordfish")}
	resp = ctx.Resource(failing)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("failing resource status = %d", resp.StatusCode)
	}
	if body, ok := resp.Body.(map[string]string); !ok || body["error"] != "internal server error" {
		t.Fatalf("failing resource body = %#v", resp.Body)
	}

	failingList := &resourceListQuery{err: errors.New("replica secret is dont-leak-me")}
	resp = ctx.Resources(failingList)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("failing resources status = %d", resp.StatusCode)
	}
	if body, ok := resp.Body.(map[string]string); !ok || body["error"] != "internal server error" {
		t.Fatalf("failing resources body = %#v", resp.Body)
	}
	if strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "dont-leak-me") {
		t.Fatalf("resource helper error log leaked detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "http error") {
		t.Fatalf("resource helper error log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedResponseWriteSetsSecureJSONHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.Response{StatusCode: http.StatusAccepted, Body: map[string]string{"ok": "true"}}.Write(rec)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}

	rec = httptest.NewRecorder()
	httpx.Response{
		StatusCode: http.StatusOK,
		Body:       "plain",
		Headers:    map[string]string{"Content-Type": "text/plain"},
	}.Write(rec)
	if got := rec.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("explicit Content-Type = %q, want text/plain", got)
	}

	rec = httptest.NewRecorder()
	httpx.Response{StatusCode: http.StatusNoContent}.Write(rec)
	if got := rec.Header().Get("Content-Type"); got != "" {
		t.Fatalf("no-content Content-Type = %q, want empty", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "" {
		t.Fatalf("no-content X-Content-Type-Options = %q, want empty", got)
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

func TestExportedGlobalRateLimitRunsBeforeMiddlewareAndHandler(t *testing.T) {
	resetGlobalRateLimitStateForTest()
	resetTrustedProxyStateForTest()
	t.Setenv("RATE_LIMIT", "true")
	t.Setenv("RATE_LIMIT_RPS", "1")
	t.Setenv("RATE_LIMIT_BURST", "1")

	var middlewareHits int
	var handlerHits int
	router := Routes(func(r *Router) {
		r.Get("/limited", func(ctx *Context) Response {
			handlerHits++
			return ctx.NoContent()
		}, func(ctx *Context, next func() Response) Response {
			middlewareHits++
			return next()
		})
	})

	first := httptest.NewRequest(http.MethodGet, "/limited", nil)
	first.RemoteAddr = "192.0.2.10:1234"
	firstRec := httptest.NewRecorder()
	router.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, body = %s", firstRec.Code, firstRec.Body.String())
	}
	if firstRec.Header().Get("X-RateLimit-Limit") == "" {
		t.Fatal("expected global rate limit headers on allowed response")
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
	if middlewareHits != 1 || handlerHits != 1 {
		t.Fatalf("global limiter should stop before middleware/handler; middleware=%d handler=%d", middlewareHits, handlerHits)
	}
}

func TestExportedGlobalRateLimitCanBeDisabled(t *testing.T) {
	resetGlobalRateLimitStateForTest()
	resetTrustedProxyStateForTest()
	t.Setenv("RATE_LIMIT", "false")
	t.Setenv("RATE_LIMIT_RPS", "1")
	t.Setenv("RATE_LIMIT_BURST", "1")

	var handlerHits int
	router := Routes(func(r *Router) {
		r.Get("/limited", func(ctx *Context) Response {
			handlerHits++
			return ctx.NoContent()
		})
	})

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.RemoteAddr = "192.0.2.20:1234"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("request %d status = %d, body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if handlerHits != 3 {
		t.Fatalf("handler hits = %d, want 3", handlerHits)
	}
}

func requestFrom(remote, xff string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	req.Header.Set("X-Forwarded-For", xff)
	return req
}

func resetGlobalRateLimitStateForTest() {
	globalLimiter = nil
	globalLimiterOnce = sync.Once{}
	rateLimitCallback = nil
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

func TestExportedLoadRolesOverridesTokenRoleFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := policies.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}

	ctx := httpx.NewContext(newRequest())
	ctx.SetAuth(&httpx.AuthInfo{UserID: "user-without-roles", Role: "admin"})
	resp := middleware.LoadRoles(ctx, func() httpx.Response {
		if got := ctx.Roles(); len(got) != 0 {
			t.Fatalf("loaded roles = %#v, want DB roles only", got)
		}
		if ctx.HasRole("admin") {
			t.Fatal("loaded empty DB roles should override token role fallback")
		}
		if ctx.IsAdmin() {
			t.Fatal("loaded empty DB roles should clear token admin fallback")
		}
		return ctx.NoContent()
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("LoadRoles response = %#v", resp)
	}
	deniedRole := middleware.RequireRole("admin")(ctx, func() httpx.Response {
		t.Fatal("RequireRole should not use stale token role after LoadRoles")
		return ctx.NoContent()
	})
	if deniedRole.StatusCode != http.StatusForbidden {
		t.Fatalf("RequireRole after empty DB roles status = %d", deniedRole.StatusCode)
	}
	deniedAdmin := middleware.RequireAdmin(ctx, func() httpx.Response {
		t.Fatal("RequireAdmin should not use stale token role after LoadRoles")
		return ctx.NoContent()
	})
	if deniedAdmin.StatusCode != http.StatusForbidden {
		t.Fatalf("RequireAdmin after empty DB roles status = %d", deniedAdmin.StatusCode)
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

	tampered := user.PrivateKeyEncrypted[:len(user.PrivateKeyEncrypted)-1] + "A"
	if tampered == user.PrivateKeyEncrypted {
		tampered = user.PrivateKeyEncrypted[:len(user.PrivateKeyEncrypted)-1] + "B"
	}
	if err := db.Model(&models.User{}).Where("id = ?", user.ID).Update("private_key_encrypted", tampered).Error; err != nil {
		t.Fatalf("tamper sealed field: %v", err)
	}
	var broken models.User
	if err := db.First(&broken, "id = ?", user.ID).Error; err == nil {
		t.Fatal("tampered sealed ciphertext should fail authentication")
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

	staleCopy1 := *account
	staleCopy2 := *account
	staleCopy1.Name = "Updated by copy1"
	if err := models.UpdateAccount(&staleCopy1); err != nil {
		t.Fatalf("first stale-version update: %v", err)
	}
	staleCopy2.Name = "Updated by copy2"
	err = models.UpdateAccount(&staleCopy2)
	if err == nil {
		t.Fatal("expected stale immutable update to fail")
	}
	if _, ok := err.(*models.StaleVersionError); !ok {
		t.Fatalf("expected StaleVersionError, got %T: %v", err, err)
	}
	if err := db.Model(&models.Account{}).Where("id = ?", account.ID).Count(&accountRows).Error; err != nil {
		t.Fatal(err)
	}
	if accountRows != 3 {
		t.Fatalf("failed stale update should not insert another version, got %d rows", accountRows)
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
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "account_controller.go"), "models.DB.Model(&models.")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "account_controller.go"), "Account{})")
	assertPathMissing(t, filepath.Join(out, "integrity_test.go"))
	assertCleanExportReport(t, out)
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
	assertCleanExportReport(t, out)
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
	assertFileContains(t, filepath.Join(out, "go.mod"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "schema:")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "- app/graphql/schema.graphqls")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/generated/generated.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: generated")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/model/models_gen.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "dir: app/graphqlapi/resolver")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: resolver")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), "//go:build tools")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), `_ "github.com/99designs/gqlgen"`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "schema.graphqls"), "type Query")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "schema.graphqls"), "type User")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "handler.New(pickleExecutableSchema{})")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "srv.AddTransport(transport.POST{})")
	assertFileNotContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "srv.AddTransport(transport.GET{})")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), `w.Header().Set("X-Content-Type-Options", "nosniff")`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func QueryUser() *UserQuery")
	assertFileNotContains(t, filepath.Join(out, "app", "graphql", "schema_gen.go"), "EMAIL_ASC")
	assertFileNotContains(t, filepath.Join(out, "app", "graphql", "schema_gen.go"), "EMAIL_DESC")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `case "email":`)
	assertFileNotContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `column = "email_encrypted"`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), `mux.Handle("/graphql", graphql.Handler())`)
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
	assertFileContains(t, filepath.Join(out, "app", "http", "requests", "bindings.go"), "package requests")
	assertCleanExportReport(t, out)
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	writeExportedZeroGraphQLEncryptedFilterTest(t, out)
	writeExportedZeroGraphQLHTTPMethodSafetyTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedZeroGraphQLEncryptedFilterTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedGraphQLQueryFiltersDeterministicEncryptedColumns(t *testing.T) {
	t.Setenv("APP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(db)
	if err := db.AutoMigrate(&User{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	now := time.Now().UTC()
	user := &User{
		ID:           uuid.New(),
		Name:         "Ada",
		Email:        "ada@example.com",
		PasswordHash: "hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := QueryUser().Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if user.EmailEncrypted == "" || user.EmailEncrypted == "ada@example.com" {
		t.Fatalf("email was not stored as ciphertext: %q", user.EmailEncrypted)
	}
	found, err := QueryUser().WhereEmail("ada@example.com").First()
	if err != nil {
		t.Fatalf("find by encrypted email: %v", err)
	}
	if found.ID != user.ID || found.Email != "ada@example.com" {
		t.Fatalf("found = %+v, want user %s with decrypted email", found, user.ID)
	}

	t.Setenv("APP_ENCRYPTION_KEY", "")
	t.Setenv("ENCRYPTION_KEY", "")
	if _, err := QueryUser().WhereEmail("ada@example.com").First(); err == nil {
		t.Fatal("encrypted filter without an encryption key should fail closed")
	}
	_ = os.Unsetenv("APP_ENCRYPTION_KEY")
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_graphql_encrypted_filter_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedZeroGraphQLHTTPMethodSafetyTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphql_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"zero-graphql/app/graphql"
	"zero-graphql/app/http/auth"
	"zero-graphql/app/http/auth/jwt"
	"zero-graphql/app/models"
)

func TestExportedGraphQLGETDoesNotExecuteMutations(t *testing.T) {
	t.Setenv("APP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.User{}, &models.JwtToken{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	auth.Init(func(key, fallback string) string {
		switch key {
		case "AUTH_DRIVER":
			return "jwt"
		case "DB_CONNECTION":
			return "sqlite"
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		default:
			return fallback
		}
	}, sqlDB)
	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{
		Subject:   uuid.New().String(),
		Role:      "admin",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	query := ` + "`" + `mutation CreateViaGET {
  createUser(input: { name: "GET Bad", email: "get@example.com" }) { id name }
}` + "`" + `
	req := httptest.NewRequest(http.MethodGet, "/graphql?query="+url.QueryEscape(query), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	graphql.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET mutation status = %d, want %d, body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}

	var count int64
	if err := db.Model(&models.User{}).Where("name = ?", "GET Bad").Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("GET mutation created %d user rows; response status=%d body=%s", count, rec.Code, rec.Body.String())
	}

	var resp struct {
		Data   any              ` + "`" + `json:"data"` + "`" + `
		Errors []map[string]any ` + "`" + `json:"errors"` + "`" + `
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v\nstatus=%d body=%s", err, rec.Code, rec.Body.String())
	}
	if len(resp.Errors) == 0 {
		t.Fatalf("GET mutation should return a GraphQL error, got status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/graphql?query="+url.QueryEscape(` + "`" + `{ users { edges { node { id } } } }` + "`" + `), nil)
	rec = httptest.NewRecorder()
	graphql.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET query status = %d, want %d, body=%s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode GET query response: %v\nstatus=%d body=%s", err, rec.Code, rec.Body.String())
	}
	if len(resp.Errors) == 0 {
		t.Fatalf("GET query should return a GraphQL error, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphql", "exported_http_method_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
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
	assertFileContains(t, filepath.Join(out, "go.mod"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "schema:")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "- app/graphql/schema.graphqls")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/generated/generated.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: generated")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/model/models_gen.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "dir: app/graphqlapi/resolver")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: resolver")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), "//go:build tools")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), `_ "github.com/99designs/gqlgen"`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "schema.graphqls"), "type Query")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "schema.graphqls"), "type User")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "handler.New(pickleExecutableSchema{})")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "srv.AddTransport(transport.POST{})")
	assertFileNotContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "srv.AddTransport(transport.GET{})")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), `w.Header().Set("X-Content-Type-Options", "nosniff")`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), `mime.ParseMediaType(contentType)`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "err.Path = graphQLErrorPath(path)")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "const maxGraphQLRequestBodyBytes = 1 << 20")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "const maxGraphQLRequestEnvelopeFieldBytes = 32")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func validateGraphQLRequestEnvelopeFieldUniqueness")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func validateGraphQLRequestEnvelopeFields")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "const maxGraphQLQueryBytes = 64 << 10")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "GraphQL query must be a string")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "const maxGraphQLOperationNameBytes = 256")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func validateGraphQLRequestEnvelope")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "const maxGraphQLVariables = 64")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "const maxGraphQLVariableNameBytes = 256")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func validateGraphQLVariables")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func validateGraphQLExtensions")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "http.MaxBytesReader(w, r.Body, maxGraphQLRequestBodyBytes)")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), "func graphQLRequestedPageLimit")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "handler_gen.go"), `parsePositivePageInt(pageArg["last"])`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "pickle_gen.go"), "maxQueryComplexity")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "pickle_gen.go"), `pageArg["last"] != nil`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "pickle_gen.go"), "var allowIntrospection = false")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func (q *UserQuery) WhereID")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Select([]string{"id", "name"})`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Select([]string{"id", "name", "email"})`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Select([]string{"id", "user_id", "title"})`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "q.db = q.db.Order(OrderClause(column, dir))")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Order(column + " " + dir)`)
	assertFileContains(t, filepath.Join(out, "app", "graphql", "resolver_gen.go"), "q.WhereCreatedAtGTE(t)")
	assertFileContains(t, filepath.Join(out, "app", "graphql", "resolver_gen.go"), "q.WhereCreatedAtLTE(t)")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `mux.Handle("/graphql", graphql.Handler())`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "routes.API.RegisterRoutes(mux)")
	assertCleanExportReport(t, out)
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	writeExportedGraphQLSafetyBehaviorTest(t, out)
	writeExportedGraphQLCostBehaviorTest(t, out)
	writeExportedGraphQLErrorBehaviorTest(t, out)
	writeExportedGraphQLRBACBehaviorTest(t, out)
	writeExportedGraphQLModelVisibilityBehaviorTest(t, out)
	runExported(t, out, "go", "run", "github.com/99designs/gqlgen", "generate", "--config", "gqlgen.yml")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "generated", "generated.go"), "type ResolverRoot interface")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *Resolver) Query() generated.QueryResolver")
	runExported(t, out, "go", "test", "./...")
}

func writeExportedGraphQLSafetyBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphql_test

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"graphql-safety/app/graphql"
	"graphql-safety/app/http/auth"
	"graphql-safety/app/http/auth/jwt"
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
		` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `,
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
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	auth.Init(func(key, fallback string) string {
		switch key {
		case "AUTH_DRIVER":
			return "jwt"
		case "DB_CONNECTION":
			return "sqlite"
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		default:
			return fallback
		}
	}, sqlDB)
	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{Subject: userID.String(), Role: "admin"})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	cases := []struct {
		name      string
		query     string
		variables map[string]any
		wantError bool
	}{
		{"allowed", ` + "`" + `query AllowedUsers {
  users(page: { first: 25 }) {
    edges { node { id name } }
    pageInfo { hasNextPage }
  }
}` + "`" + `, nil, false},
		{"huge_first", ` + "`" + `query HugeFirst {
  users(page: { first: 101 }) { edges { node { id } } }
}` + "`" + `, nil, true},
		{"huge_first_variable", ` + "`" + `query HugeFirstVariable($first: Int) {
  users(page: { first: $first }) { edges { node { id } } }
}` + "`" + `, map[string]any{"first": 101}, true},
		{"introspection_disabled", ` + "`" + `query IntrospectionDisabled {
  __schema { queryType { name } }
}` + "`" + `, nil, true},
		{"multi_operation", ` + "`" + `query One { users { edges { node { id } } } }
query Two { posts { edges { node { id } } } }` + "`" + `, nil, true},
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
}` + "`" + `, nil, true},
		{"unexposed_create", ` + "`" + `mutation UnexposedCreate {
  createUser(input: { name: "bad", email: "bad@example.com" }) { id }
}` + "`" + `, nil, true},
		{"unexposed_delete", ` + "`" + `mutation UnexposedDelete {
  deleteUser(id: "00000000-0000-0000-0000-000000000001")
}` + "`" + `, nil, true},
		{"relationship_fanout", ` + "`" + `query RelationshipFanout {
  users(page: { first: 100 }) {
    edges { node { posts { comments { body } } } }
  }
}` + "`" + `, nil, true},
	}

	handler := graphql.Handler()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"query": tc.query, "variables": tc.variables})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
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

	t.Run("named_fragments_execute_and_count_selected_fields", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"query": ` + "`" + `query FragmentedUsers {
  users(page: { first: 25 }) {
    edges { node { ...UserFields } }
  }
}

fragment UserFields on User {
  id
  name
}` + "`" + `})
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
			Data struct {
				Users struct {
					Edges []struct {
						Node map[string]any ` + "`" + `json:"node"` + "`" + `
					} ` + "`" + `json:"edges"` + "`" + `
				} ` + "`" + `json:"users"` + "`" + `
			} ` + "`" + `json:"data"` + "`" + `
			Errors []map[string]any ` + "`" + `json:"errors"` + "`" + `
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
		}
		if len(resp.Errors) != 0 {
			t.Fatalf("unexpected GraphQL errors: %v\n%s", resp.Errors, rec.Body.String())
		}
		if len(resp.Data.Users.Edges) != 1 {
			t.Fatalf("edges = %d, body = %s", len(resp.Data.Users.Edges), rec.Body.String())
		}
		node := resp.Data.Users.Edges[0].Node
		if node["name"] != "Ada" {
			t.Fatalf("fragment fields were not executed: %#v body=%s", node, rec.Body.String())
		}
	})

	t.Run("authenticated_fields_use_exported_auth_driver", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"query": ` + "`" + `query AuthenticatedUser($id: ID!) {
  user(id: $id) { id email name }
}` + "`" + `, "variables": map[string]any{"id": userID.String()}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "\"errors\"") {
			t.Fatalf("unexpected authenticated GraphQL errors: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), userID.String()) || !strings.Contains(rec.Body.String(), "ada@example.com") {
			t.Fatalf("authenticated GraphQL response did not include auth-only fields: %s", rec.Body.String())
		}
	})

	t.Run("auth_failures_are_sanitized", func(t *testing.T) {
		var logs bytes.Buffer
		previousLogOutput := log.Writer()
		log.SetOutput(&logs)
		defer log.SetOutput(previousLogOutput)

		body, err := json.Marshal(map[string]any{"query": ` + "`" + `query AuthFailure($id: ID!) {
  user(id: $id) { id email name }
}` + "`" + `, "variables": map[string]any{"id": userID.String()}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer dont-leak-me")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "dont-leak-me") || strings.Contains(rec.Body.String(), "jwt:") {
			t.Fatalf("auth failure leaked implementation details: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unauthenticated") {
			t.Fatalf("missing sanitized auth error: %s", rec.Body.String())
		}
		if strings.Contains(logs.String(), "dont-leak-me") || strings.Contains(logs.String(), "jwt:") {
			t.Fatalf("auth failure leaked implementation details in logs: %s", logs.String())
		}
		if !strings.Contains(logs.String(), "graphql auth failed") {
			t.Fatalf("auth failure log missing sanitized marker: %s", logs.String())
		}
	})

	t.Run("oversized_auth_header_is_sanitized", func(t *testing.T) {
		var logs bytes.Buffer
		previousLogOutput := log.Writer()
		log.SetOutput(&logs)
		defer log.SetOutput(previousLogOutput)

		body, err := json.Marshal(map[string]any{"query": ` + "`" + `query AuthFailure($id: ID!) {
  user(id: $id) { id email name }
}` + "`" + `, "variables": map[string]any{"id": userID.String()}})
		if err != nil {
			t.Fatal(err)
		}
		hugeHeader := "Bearer " + strings.Repeat("x", 13<<10)
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Authorization", hugeHeader)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unauthenticated") {
			t.Fatalf("missing sanitized auth error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) || strings.Contains(logs.String(), strings.Repeat("x", 128)) {
			t.Fatalf("oversized auth header leaked in response/logs: body=%s logs=%s", rec.Body.String(), logs.String())
		}
		if !strings.Contains(logs.String(), "graphql auth failed") {
			t.Fatalf("auth failure log missing sanitized marker: %s", logs.String())
		}
	})

	t.Run("oversized_request_body_rejected_before_parse", func(t *testing.T) {
		body := strings.Repeat(" ", (1<<20)+1)
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("oversized Content-Type = %q, want application/json", got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("oversized X-Content-Type-Options = %q, want nosniff", got)
		}
		if !strings.Contains(rec.Body.String(), "graphql request body too large") {
			t.Fatalf("oversized body missing sanitized error: %s", rec.Body.String())
		}
	})

	t.Run("unknown_length_oversized_request_body_rejected_before_parse", func(t *testing.T) {
		body := strings.Repeat(" ", (1<<20)+1)
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.ContentLength = -1
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("streaming oversized Content-Type = %q, want application/json", got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("streaming oversized X-Content-Type-Options = %q, want nosniff", got)
		}
		if !strings.Contains(rec.Body.String(), "graphql request body too large") {
			t.Fatalf("streaming oversized body missing sanitized error: %s", rec.Body.String())
		}
	})

	t.Run("unsupported_envelope_field_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query": "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"debug": "dont-leak-me",
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL request contains unsupported field") {
			t.Fatalf("missing unsupported envelope field error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "debug") || strings.Contains(rec.Body.String(), "dont-leak-me") {
			t.Fatalf("unsupported envelope field error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("duplicate_envelope_field_rejected_before_parse", func(t *testing.T) {
		body := ` + "`" + `{
  "query": "query First { users(page: { first: 1 }) { edges { node { id } } } }",
  "query": "query Second { users(page: { first: 1 }) { edges { node { name } } } }"
}` + "`" + `
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL request contains duplicate field") {
			t.Fatalf("missing duplicate envelope field error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Second") || strings.Contains(rec.Body.String(), "name") {
			t.Fatalf("duplicate envelope field error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("oversized_envelope_field_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query": "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			strings.Repeat("x", 33): "value",
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL request field is too large") {
			t.Fatalf("missing oversized envelope field error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 16)) {
			t.Fatalf("oversized envelope field error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("oversized_query_string_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"query": "query TooLarge { " + strings.Repeat("x", 64<<10) + " }"})
		if err != nil {
			t.Fatal(err)
		}
		if len(body) >= 1<<20 {
			t.Fatalf("test body should stay below request cap, got %d bytes", len(body))
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL query is too large") {
			t.Fatalf("missing query size error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
			t.Fatalf("query size error leaked oversized query: %s", rec.Body.String())
		}
	})

	t.Run("missing_query_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"variables": map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL query must be a string") {
			t.Fatalf("missing query error = %s", rec.Body.String())
		}
	})

	t.Run("non_string_query_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"query": []any{"not", "a", "string"}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL query must be a string") {
			t.Fatalf("non-string query error = %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "not") {
			t.Fatalf("query type error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("blank_query_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"query": " \n\t "})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL query must not be empty") {
			t.Fatalf("blank query error = %s", rec.Body.String())
		}
	})

	t.Run("oversized_operation_name_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":         "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"operationName": strings.Repeat("x", 257),
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL operationName is too large") {
			t.Fatalf("missing operationName size error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
			t.Fatalf("operationName size error leaked oversized value: %s", rec.Body.String())
		}
	})

	t.Run("non_string_operation_name_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":         "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"operationName": []any{"not", "a", "string"},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL operationName must be a string") {
			t.Fatalf("missing operationName type error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "not") {
			t.Fatalf("operationName type error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("invalid_operation_name_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":         "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"operationName": "1 invalid",
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL operationName is invalid") {
			t.Fatalf("missing operationName syntax error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "1 invalid") {
			t.Fatalf("operationName syntax error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("malformed_request_body_does_not_echo_body", func(t *testing.T) {
		body := ` + "`" + `{"query": "{ users { edges { node { id } } }", "secret": "dont-leak-me"` + "`" + `
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "dont-leak-me") || strings.Contains(rec.Body.String(), body) {
			t.Fatalf("malformed body leaked in response: %s", rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid GraphQL request body") {
			t.Fatalf("missing sanitized body error: %s", rec.Body.String())
		}
	})

	t.Run("trailing_json_rejected_before_parse", func(t *testing.T) {
		body := ` + "`" + `{"query":"{ users(page: { first: 1 }) { edges { node { id } } } }"} {"secret":"dont-leak-me"}` + "`" + `
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "invalid GraphQL request body") {
			t.Fatalf("missing sanitized trailing JSON error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "dont-leak-me") || strings.Contains(rec.Body.String(), body) {
			t.Fatalf("trailing JSON leaked in response: %s", rec.Body.String())
		}
	})

	t.Run("json_content_type_with_charset_is_accepted", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{"query": ` + "`" + `query CharsetJSON {
  users(page: { first: 1 }) { edges { node { id } } }
}` + "`" + `})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "\"errors\"") {
			t.Fatalf("charset JSON request should execute without errors: %s", rec.Body.String())
		}
	})

	t.Run("non_json_content_type_rejected_before_parse", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(` + "`" + `{"query":"{ users { edges { node { id } } } }"}` + "`" + `))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnsupportedMediaType, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("non-json Content-Type = %q, want application/json", got)
		}
		if !strings.Contains(rec.Body.String(), "GraphQL POST requests require application/json") {
			t.Fatalf("missing sanitized media type error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "text/plain") {
			t.Fatalf("media type rejection leaked request content type: %s", rec.Body.String())
		}
	})

	t.Run("oversized_variable_string_rejected", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query": ` + "`" + `query OversizedVariable($id: ID!) {
  user(id: $id) { id name }
}` + "`" + `,
			"variables": map[string]any{"id": strings.Repeat("x", 4097)},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL variables exceed safety limits") {
			t.Fatalf("missing variable budget error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
			t.Fatalf("variable budget error leaked oversized variable: %s", rec.Body.String())
		}
	})

	t.Run("oversized_variable_name_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":     "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"variables": map[string]any{strings.Repeat("x", 257): "value"},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL variable name is too large") {
			t.Fatalf("missing variable name size error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
			t.Fatalf("variable name size error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("invalid_variable_name_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":     "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"variables": map[string]any{"1 invalid": "value"},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL variable name is invalid") {
			t.Fatalf("missing variable name syntax error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "1 invalid") {
			t.Fatalf("variable name syntax error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("non_object_variables_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":     "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"variables": []any{"not", "an", "object"},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL variables must be an object") {
			t.Fatalf("missing variables type error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "not") {
			t.Fatalf("variables type error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("null_variables_are_accepted", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":     "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"variables": nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "\"errors\"") {
			t.Fatalf("null variables should execute without errors: %s", rec.Body.String())
		}
	})

	t.Run("non_object_extensions_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":      "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"extensions": []any{"not", "an", "object"},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL extensions must be an object") {
			t.Fatalf("missing extensions type error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "not") {
			t.Fatalf("extensions type error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("oversized_extensions_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":      "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"extensions": map[string]any{"trace": strings.Repeat("x", 4097)},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL extensions exceed safety limits") {
			t.Fatalf("missing extensions budget error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
			t.Fatalf("extensions budget error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("oversized_extension_name_rejected_before_parse", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":      "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"extensions": map[string]any{strings.Repeat("x", 257): "value"},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "GraphQL extension name is too large") {
			t.Fatalf("missing extension name size error: %s", rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
			t.Fatalf("extension name size error leaked request value: %s", rec.Body.String())
		}
	})

	t.Run("null_extensions_are_accepted", func(t *testing.T) {
		body, err := json.Marshal(map[string]any{
			"query":      "query AllowedUsers { users(page: { first: 1 }) { edges { node { id } } } }",
			"extensions": nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "\"errors\"") {
			t.Fatalf("null extensions should execute without errors: %s", rec.Body.String())
		}
	})

	t.Run("batched_json_requests_are_rejected", func(t *testing.T) {
		body := ` + "`" + `[
  { "query": "{ users { edges { node { id } } } }" },
  { "query": "{ posts { edges { node { id } } } }" }
]` + "`" + `
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "batched GraphQL requests are not supported") {
			t.Fatalf("missing batch rejection: %s", rec.Body.String())
		}
	})

	t.Run("sdl_endpoint_follows_introspection_gate", func(t *testing.T) {
		graphql.SetIntrospection(false)
		req := httptest.NewRequest(http.MethodGet, "/graphql?sdl=1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("disabled SDL status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
		}

		graphql.SetIntrospection(true)
		defer graphql.SetIntrospection(false)
		req = httptest.NewRequest(http.MethodGet, "/graphql?sdl=1", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("enabled SDL status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "type Query") {
			t.Fatalf("enabled SDL body did not include schema: %s", rec.Body.String())
		}
	})
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphql", "exported_safety_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedGraphQLCostBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphql

import (
	"context"
	"strings"
	"testing"
)

func TestExportedGraphQLCostModelPricesBackwardPagination(t *testing.T) {
	cost := FieldCost{TypeName: "Query", FieldName: "users", BaseCost: 3, IsList: true, MaxLimit: 100}
	first, err := fieldComplexity(Field{Name: "users", Args: map[string]any{"page": map[string]any{"first": 40}}}, cost)
	if err != nil {
		t.Fatalf("first complexity: %v", err)
	}
	last, err := fieldComplexity(Field{Name: "users", Args: map[string]any{"page": map[string]any{"last": 40}}}, cost)
	if err != nil {
		t.Fatalf("last complexity: %v", err)
	}
	if first != 120 || last != first {
		t.Fatalf("first/last complexity = %d/%d, want both 120", first, last)
	}
	if _, err := fieldComplexity(Field{Name: "users", Args: map[string]any{"page": map[string]any{"last": 101}}}, cost); err == nil || !strings.Contains(err.Error(), "page.last 101 exceeds maximum 100") {
		t.Fatalf("last over-limit error = %v", err)
	}

	previous, hadPrevious := generatedFieldCosts["Query.users"]
	generatedFieldCosts["Query.users"] = cost
	defer func() {
		if hadPrevious {
			generatedFieldCosts["Query.users"] = previous
		} else {
			delete(generatedFieldCosts, "Query.users")
		}
	}()
	got, ok := (pickleExecutableSchema{}).Complexity(context.Background(), "Query", "users", 7, map[string]any{"page": map[string]any{"last": 40}})
	if !ok {
		t.Fatal("schema complexity did not handle Query.users")
	}
	if got != 127 {
		t.Fatalf("schema complexity with page.last = %d, want child 7 + list cost 120", got)
	}
}

func TestExportedGraphQLVariableBudget(t *testing.T) {
	if err := validateGraphQLVariables(map[string]any{
		"ok": map[string]any{"items": []any{"one", "two"}},
	}); err != nil {
		t.Fatalf("valid variables rejected: %v", err)
	}

	tooMany := map[string]any{}
	for i := 0; i < maxGraphQLVariables+1; i++ {
		tooMany[string(rune('a'+i))] = i
	}
	if err := validateGraphQLVariables(tooMany); err == nil || !strings.Contains(err.Error(), "too many GraphQL variables") {
		t.Fatalf("too many variables error = %v", err)
	}

	if err := validateGraphQLVariables(map[string]any{strings.Repeat("x", maxGraphQLVariableNameBytes+1): "value"}); err == nil || !strings.Contains(err.Error(), "GraphQL variable name is too large") {
		t.Fatalf("oversized variable name error = %v", err)
	}

	if err := validateGraphQLVariables(map[string]any{"1 invalid": "value"}); err == nil || !strings.Contains(err.Error(), "GraphQL variable name is invalid") {
		t.Fatalf("invalid variable name error = %v", err)
	}

	if err := validateGraphQLVariables(map[string]any{"s": strings.Repeat("x", maxGraphQLVariableStringBytes+1)}); err == nil || !strings.Contains(err.Error(), "GraphQL variables exceed safety limits") {
		t.Fatalf("oversized string variable error = %v", err)
	}

	wide := make([]any, maxGraphQLVariableCollectionItems+1)
	if err := validateGraphQLVariables(map[string]any{"wide": wide}); err == nil || !strings.Contains(err.Error(), "GraphQL variables exceed safety limits") {
		t.Fatalf("wide variable error = %v", err)
	}

	var deep any = "leaf"
	for i := 0; i <= maxGraphQLVariableDepth; i++ {
		deep = map[string]any{"next": deep}
	}
	if err := validateGraphQLVariables(map[string]any{"deep": deep}); err == nil || !strings.Contains(err.Error(), "GraphQL variables exceed safety limits") {
		t.Fatalf("deep variable error = %v", err)
	}
}

func TestExportedGraphQLExtensionsBudget(t *testing.T) {
	if err := validateGraphQLExtensions(map[string]any{
		"trace": map[string]any{"sampled": true},
	}); err != nil {
		t.Fatalf("valid extensions rejected: %v", err)
	}

	wide := map[string]any{}
	for i := 0; i < maxGraphQLVariableCollectionItems+1; i++ {
		wide[string(rune('a'+i))] = true
	}
	if err := validateGraphQLExtensions(wide); err == nil || !strings.Contains(err.Error(), "GraphQL extensions exceed safety limits") {
		t.Fatalf("wide extensions error = %v", err)
	}

	if err := validateGraphQLExtensions(map[string]any{strings.Repeat("x", maxGraphQLVariableNameBytes+1): "value"}); err == nil || !strings.Contains(err.Error(), "GraphQL extension name is too large") {
		t.Fatalf("oversized extension name error = %v", err)
	}

	if err := validateGraphQLExtensions(map[string]any{"trace": strings.Repeat("x", maxGraphQLVariableStringBytes+1)}); err == nil || !strings.Contains(err.Error(), "GraphQL extensions exceed safety limits") {
		t.Fatalf("oversized extension value error = %v", err)
	}
}

func TestExportedGraphQLOperationNameSyntax(t *testing.T) {
	for _, name := range []string{"AllowedUsers", "_AllowedUsers", "Allowed_Users1"} {
		if !isGraphQLName(name) {
			t.Fatalf("isGraphQLName(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "1AllowedUsers", "Allowed Users", "Allowed-Users", "Allowed.Users"} {
		if isGraphQLName(name) {
			t.Fatalf("isGraphQLName(%q) = true, want false", name)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphql", "exported_cost_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedGraphQLErrorBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphql

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"testing"
)

func TestExportedGraphQLErrorsPreservePath(t *testing.T) {
	errs := convertGraphQLErrors([]map[string]any{
		{
			"message": "user not found",
			"path":    []string{"user"},
			"extensions": map[string]any{
				"code": "NOT_FOUND",
			},
		},
		{
			"message": "nested field denied",
			"path":    []any{"users", 0, "email"},
		},
	})
	if len(errs) != 2 {
		t.Fatalf("len(errs) = %d, want 2", len(errs))
	}
	if errs[0].Message != "user not found" {
		t.Fatalf("message = %q", errs[0].Message)
	}
	if errs[0].Path.String() != "user" {
		t.Fatalf("path = %q, want user", errs[0].Path.String())
	}
	if errs[0].Extensions["code"] != "NOT_FOUND" {
		t.Fatalf("extensions = %#v", errs[0].Extensions)
	}
	if errs[1].Path.String() != "users[0].email" {
		t.Fatalf("nested path = %q, want users[0].email", errs[1].Path.String())
	}
}

func TestExportedGraphQLExecutionRecoversPanics(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	data, errs := executeSafely(&ResolveContext{}, panicRoot{}, &Document{
		Operation: "query",
		Fields: []Field{{Name: "boom"}},
	})
	if data != nil {
		t.Fatalf("data = %#v, want nil after panic", data)
	}
	if len(errs) != 1 {
		t.Fatalf("len(errs) = %d, want 1", len(errs))
	}
	if errs[0]["message"] != "internal server error" {
		t.Fatalf("panic message leaked or changed: %#v", errs[0])
	}
	extensions, ok := errs[0]["extensions"].(map[string]any)
	if !ok || extensions["code"] != CodeInternalServerError {
		t.Fatalf("panic extensions = %#v", errs[0]["extensions"])
	}
	if strings.Contains(logs.String(), "secret panic detail") {
		t.Fatalf("panic log leaked detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "graphql panic recovered") {
		t.Fatalf("panic log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedGraphQLInternalErrorsAreSanitized(t *testing.T) {
	err := toGraphQLError(InternalError("secret database detail"), []string{"createUser"})
	if err["message"] != "internal server error" {
		t.Fatalf("internal error message = %#v", err)
	}
	extensions, ok := err["extensions"].(map[string]any)
	if !ok || extensions["code"] != CodeInternalServerError {
		t.Fatalf("internal error extensions = %#v", err["extensions"])
	}
	if strings.Contains(fmt.Sprint(err), "secret database detail") {
		t.Fatalf("internal error leaked detail: %#v", err)
	}

	plain := toGraphQLError(errors.New("database password is swordfish"), []string{"users"})
	if plain["message"] != "internal server error" {
		t.Fatalf("plain error message = %#v, want sanitized internal server error", plain["message"])
	}
	plainExtensions, ok := plain["extensions"].(map[string]any)
	if !ok || plainExtensions["code"] != CodeInternalServerError {
		t.Fatalf("plain error extensions = %#v", plain["extensions"])
	}
	if strings.Contains(fmt.Sprint(plain), "swordfish") {
		t.Fatalf("plain internal error leaked detail: %#v", plain)
	}
}

func TestExportedGraphQLGQLGenInternalResponsesAreSanitized(t *testing.T) {
	resp := gqlgenErrorResponse("secret marshal detail", CodeInternalServerError)
	if len(resp.Errors) != 1 {
		t.Fatalf("errors = %#v", resp.Errors)
	}
	if resp.Errors[0].Message != "internal server error" {
		t.Fatalf("gqlgen internal message = %q", resp.Errors[0].Message)
	}
	if strings.Contains(fmt.Sprint(resp.Errors[0]), "secret marshal detail") {
		t.Fatalf("gqlgen internal response leaked detail: %#v", resp.Errors[0])
	}
}

func TestExportedGraphQLHTTPWriteFailuresAreSanitized(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	writeGraphQLHTTPStatusError(failingGraphQLWriter{}, http.StatusOK, "bad input", CodeBadUserInput)
	if strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "password") {
		t.Fatalf("write failure log leaked detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "graphql: failed to write error response") {
		t.Fatalf("write failure log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedGraphQLGQLGenRecoverHookIsSanitized(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	err := graphQLRecover(context.Background(), "secret library panic")
	if err == nil {
		t.Fatal("recover hook returned nil error")
	}
	if strings.Contains(fmt.Sprint(err), "secret library panic") {
		t.Fatalf("recover hook leaked panic detail in error: %#v", err)
	}

	presented := graphQLErrorPresenter(context.Background(), err)
	if presented == nil {
		t.Fatal("presenter returned nil")
	}
	if presented.Message != "internal server error" {
		t.Fatalf("presented message = %q, want sanitized internal server error", presented.Message)
	}
	if presented.Extensions["code"] != CodeInternalServerError {
		t.Fatalf("presented extensions = %#v", presented.Extensions)
	}
	if strings.Contains(fmt.Sprint(presented), "secret library panic") {
		t.Fatalf("presenter leaked panic detail: %#v", presented)
	}
	if strings.Contains(logs.String(), "secret library panic") {
		t.Fatalf("recover log leaked panic detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "graphql panic recovered") {
		t.Fatalf("recover log missing sanitized marker: %s", logs.String())
	}
}

type panicRoot struct{}

func (panicRoot) resolveQuery(ctx *ResolveContext, field Field) (any, error) {
	panic("secret panic detail")
}

func (panicRoot) resolveMutation(ctx *ResolveContext, field Field) (any, error) {
	panic("secret panic detail")
}

type failingGraphQLWriter struct{}

func (failingGraphQLWriter) Header() http.Header { return http.Header{} }
func (failingGraphQLWriter) WriteHeader(status int) {}
func (failingGraphQLWriter) Write(p []byte) (int, error) {
	return 0, errors.New("broken pipe password=swordfish")
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphql", "exported_error_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedGraphQLRBACBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphql

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"graphql-safety/app/http/auth"
	"graphql-safety/app/http/auth/jwt"
	"graphql-safety/app/models"
)

func TestExportedGraphQLRBACClaimsLoadDatabaseRoles(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	for _, stmt := range []string{
		` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE roles (id TEXT PRIMARY KEY, slug TEXT NOT NULL, manages BOOLEAN NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE role_user (user_id TEXT NOT NULL, role_id TEXT NOT NULL)` + "`" + `,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}

	userID := uuid.New().String()
	if err := db.Exec("INSERT INTO roles (id, slug, manages) VALUES (?, ?, ?)", "role-manager", "tenant_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO role_user (user_id, role_id) VALUES (?, ?)", userID, "role-manager").Error; err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	auth.Init(func(key, fallback string) string {
		switch key {
		case "AUTH_DRIVER":
			return "jwt"
		case "DB_CONNECTION":
			return "sqlite"
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		default:
			return fallback
		}
	}, sqlDB)
	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{Subject: userID, Role: "viewer"})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	claims, err := extractAuthFromHeaders(http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatalf("extract auth: %v", err)
	}
	if claims == nil {
		t.Fatal("expected claims")
	}
	if claims.UserID != userID {
		t.Fatalf("UserID = %q, want %q", claims.UserID, userID)
	}
	if claims.Role != "viewer" {
		t.Fatalf("Role = %q, want token fallback role", claims.Role)
	}
	if !claims.Manages {
		t.Fatalf("expected managing role from role_user, got %+v", claims)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "tenant_admin" {
		t.Fatalf("Roles = %+v, want tenant_admin", claims.Roles)
	}

	ctx := &ResolveContext{auth: claims}
	if !ctx.HasRole("tenant_admin") {
		t.Fatal("HasRole did not use loaded RBAC roles")
	}
	if !ctx.CanSeeOwnerFields(uuid.New().String()) {
		t.Fatal("managing RBAC role should see owner fields")
	}
	if got := ctx.Visibility(); got != VisibilityAll {
		t.Fatalf("Visibility() = %v, want VisibilityAll", got)
	}
}

func TestExportedGraphQLRBACMissingTablesDoNotBreakPlainAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.Exec(` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	auth.Init(func(key, fallback string) string {
		switch key {
		case "AUTH_DRIVER":
			return "jwt"
		case "DB_CONNECTION":
			return "sqlite"
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		default:
			return fallback
		}
	}, sqlDB)
	userID := uuid.New().String()
	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{Subject: userID, Role: "editor", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	claims, err := extractAuthFromHeaders(http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatalf("extract auth: %v", err)
	}
	if claims == nil || claims.UserID != userID || claims.Role != "editor" {
		t.Fatalf("claims = %+v, want plain token claims", claims)
	}
	if claims.RBACLoaded {
		t.Fatalf("RBACLoaded = true with missing RBAC tables: %+v", claims)
	}
	ctx := &ResolveContext{auth: claims}
	if !ctx.HasRole("editor") {
		t.Fatal("missing RBAC tables should preserve token role fallback")
	}
}

func TestExportedGraphQLRBACEmptyTablesOverrideTokenRoleFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	for _, stmt := range []string{
		` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE roles (id TEXT PRIMARY KEY, slug TEXT NOT NULL, manages BOOLEAN NOT NULL)` + "`" + `,
		` + "`" + `CREATE TABLE role_user (user_id TEXT NOT NULL, role_id TEXT NOT NULL)` + "`" + `,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	auth.Init(func(key, fallback string) string {
		switch key {
		case "AUTH_DRIVER":
			return "jwt"
		case "DB_CONNECTION":
			return "sqlite"
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		default:
			return fallback
		}
	}, sqlDB)
	userID := uuid.New().String()
	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{Subject: userID, Role: "admin", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	claims, err := extractAuthFromHeaders(http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatalf("extract auth: %v", err)
	}
	if claims == nil || !claims.RBACLoaded {
		t.Fatalf("claims = %+v, want RBAC-loaded claims", claims)
	}
	ctx := &ResolveContext{auth: claims}
	if ctx.HasRole("admin") {
		t.Fatal("empty DB roles should override token admin role")
	}
	if ctx.CanSeeOwnerFields(uuid.New().String()) {
		t.Fatal("empty DB roles should override token admin owner visibility")
	}
	if got := ctx.Visibility(); got != VisibilityOwner {
		t.Fatalf("Visibility() = %v, want VisibilityOwner after empty DB roles", got)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphql", "exported_rbac_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedGraphQLModelVisibilityBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedGraphQLVisibilitySelectsProjectedColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(db)

	if got, want := QueryUser().SelectPublic().db.Statement.Selects, []string{"id", "name"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("User SelectPublic columns = %#v, want %#v", got, want)
	}
	if got, want := QueryUser().SelectOwner().db.Statement.Selects, []string{"id", "name", "email"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("User SelectOwner columns = %#v, want %#v", got, want)
	}
	if got, want := QueryPost().SelectPublic().db.Statement.Selects, []string{"id", "user_id", "title"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Post SelectPublic columns = %#v, want %#v", got, want)
	}
}

func TestExportedGraphQLOrderByFailsClosed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(db)

	safe := QueryUser().OrderBy("created_at", " desc ")
	if len(safe.db.Statement.Clauses) == 0 {
		t.Fatalf("safe OrderBy did not add an order clause")
	}

	unknown := QueryUser().OrderBy("created_at; DROP TABLE users", "DESC")
	if len(unknown.db.Statement.Clauses) != 0 {
		t.Fatalf("unknown GraphQL order column should be ignored, got %#v", unknown.db.Statement.Clauses)
	}

	unsafeDirection := QueryUser().OrderBy("created_at", "DESC; DROP TABLE users")
	var users []User
	stmt := unsafeDirection.db.Session(&gorm.Session{DryRun: true}).Find(&users).Statement
	sql := stmt.SQL.String()
	if strings.Contains(sql, "DROP TABLE") || strings.Contains(sql, "DESC;") {
		t.Fatalf("unsafe direction leaked into SQL: %s", sql)
	}
	if !strings.Contains(sql, "ASC") {
		t.Fatalf("unsafe direction did not normalize to ASC: %s", sql)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_graphql_visibility_test.go"), []byte(testSrc), 0o644); err != nil {
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
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "ReadHeaderTimeout: 10 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "ReadTimeout:       30 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "WriteTimeout:      60 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "IdleTimeout:       120 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "MaxHeaderBytes:    1 << 20")
	assertCleanExportReport(t, out)
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
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
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

func (*exportedCronError) Error() string { return "database password is swordfish" }

type exportedPanicJob struct {
	attempts int32
}

func (j *exportedPanicJob) Handle() error {
	atomic.AddInt32(&j.attempts, 1)
	panic("secret job panic")
}

func TestExportedSchedulerOptionsAndRetries(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

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
	if strings.Contains(logs.String(), "swordfish") {
		t.Fatalf("scheduler retry log leaked error detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "job failed") {
		t.Fatalf("scheduler retry log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedSchedulerRejectsInvalidSchedulesWithoutLeakingSpec(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	scheduler := Cron(func(s *Scheduler) {
		s.Job("password=swordfish", &exportedFlakyJob{})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.Start(ctx)
	if strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "password=") || strings.Contains(logs.String(), "expected exactly") {
		t.Fatalf("invalid schedule log leaked detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "schedule rejected") {
		t.Fatalf("invalid schedule log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedSchedulerRecoversJobPanics(t *testing.T) {
	job := &exportedPanicJob{}
	err := safeHandleJob(job)
	if err == nil {
		t.Fatal("expected recovered panic error")
	}
	if got := atomic.LoadInt32(&job.attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
	if err.Error() != "job panic" {
		t.Fatalf("panic error = %v, want sanitized job panic", err)
	}
	if strings.Contains(err.Error(), "secret job panic") {
		t.Fatalf("panic error leaked secret detail: %v", err)
	}
	runJob(&JobEntry{Job: job, maxRetries: 1})
	if got := atomic.LoadInt32(&job.attempts); got != 3 {
		t.Fatalf("panic job retry attempts = %d, want 3", got)
	}
}

func TestExportedSchedulerRecoversTimedJobPanics(t *testing.T) {
	job := &panicAfterErrorJob{}
	runJob(&JobEntry{Job: job, maxRetries: 1, timeout: time.Second})
	if got := atomic.LoadInt32(&job.attempts); got != 2 {
		t.Fatalf("timed panic job attempts = %d, want 2", got)
	}
}

type panicAfterErrorJob struct {
	attempts int32
}

func (j *panicAfterErrorJob) Handle() error {
	attempt := atomic.AddInt32(&j.attempts, 1)
	if attempt == 1 {
		return fmt.Errorf("first failure")
	}
	panic("timed panic")
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
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "ReadHeaderTimeout: 10 * time.Second")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "MaxHeaderBytes:    1 << 20")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Cron job scheduler support with exported server startup wiring")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Standalone command dispatch with embedded SQL migration commands")
	assertCleanExportReport(t, out)
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

func TestExportFailsUngatedActionsBeforeWritingBrokenWiring(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	actionsDir := filepath.Join(projectDir, "database", "actions", "user")
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	action := `package user

import (
	models "github.com/shortontech/pickle/testdata/basic-crud/app/models"
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type SuspendAction struct{}

func (a SuspendAction) Suspend(ctx *pickle.Context, user *models.User) error {
	user.Name = "suspended"
	return models.QueryUser().Update(user)
}
`
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil || !strings.Contains(err.Error(), "actions without gates in user: Suspend") {
		t.Fatalf("Export error = %v, want ungated action failure", err)
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

func TestCreateViewSQLUsesEncryptedStorageColumns(t *testing.T) {
	users := &schema.Table{Name: "users", Columns: []*schema.Column{
		{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
		{Name: "email", Type: schema.String, IsEncrypted: true},
	}}
	view := &schema.View{Name: "user_emails"}
	view.From("users", "u")
	view.Column("u.id")
	view.Column("u.email")
	view.GroupBy("u.id", "u.email")

	sql := createViewSQL(view, users)
	for _, want := range []string{
		`"u"."email_encrypted" AS "email"`,
		`GROUP BY u.id, u.email_encrypted`,
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("view SQL missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, `"u"."email",`) {
		t.Fatalf("view SQL retained logical encrypted column:\n%s", sql)
	}
}

func TestGraphQLQuerySupportUsesEncryptedStorageAndFailsClosed(t *testing.T) {
	var b strings.Builder
	writeGraphQLModelQuerySupport(&b, "users", []*schema.Column{
		{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
		{Name: "email", Type: schema.String, IsEncrypted: true},
		{Name: "private_key", Type: schema.String, IsSealed: true},
	}, false)
	src := b.String()
	for _, want := range []string{
		`WhereEmail(value any) *UserQuery { q.db = graphQLEncryptedWhere(q.db, "email_encrypted", "=", value); return q }`,
		`encrypted column email does not support Like filters`,
		`sealed column private_key cannot be filtered`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("GraphQL query support missing %q:\n%s", want, src)
		}
	}
	for _, unexpected := range []string{
		`q.db.Where("email = ?`,
		`q.db.Where("private_key_encrypted = ?`,
		`case "email":`,
		`case "private_key":`,
		`column = "email_encrypted"`,
	} {
		if strings.Contains(src, unexpected) {
			t.Fatalf("GraphQL query support retained unsafe encrypted/sealed behavior %q:\n%s", unexpected, src)
		}
	}
	if !strings.Contains(src, "\tdefault:\n\t\treturn q\n") {
		t.Fatalf("GraphQL query support should ignore unsupported sort columns:\n%s", src)
	}
	if strings.Contains(src, `q.db.Where("email = ?`) || strings.Contains(src, `q.db.Where("private_key_encrypted = ?`) {
		t.Fatalf("GraphQL query support retained unsafe encrypted/sealed predicates:\n%s", src)
	}
}

func TestExportCapturedAlterMigrationsRunInSQLite(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "zero-graphql"))
	migrationsDir := filepath.Join(projectDir, "database", "migrations")
	createWidget := `package migrations

type CreateWidgetsTable_2026_04_01_100000 struct {
	Migration
}

func (m *CreateWidgetsTable_2026_04_01_100000) Up() {
	m.CreateTable("widgets", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.String("name", 255).NotNull()
	})
}

func (m *CreateWidgetsTable_2026_04_01_100000) Down() {
	m.DropTableIfExists("widgets")
}
`
	alterWidget := `package migrations

type AlterWidgetsTable_2026_04_01_100001 struct {
	Migration
}

func (m *AlterWidgetsTable_2026_04_01_100001) Up() {
	m.RenameColumn("widgets", "name", "label")
	m.AlterTable("widgets", func(t *Table) {
		t.String("slug", 255).Nullable()
	})
}

func (m *AlterWidgetsTable_2026_04_01_100001) Down() {
	m.DropColumn("widgets", "slug")
	m.RenameColumn("widgets", "label", "name")
}
`
	if err := os.WriteFile(filepath.Join(migrationsDir, "2026_04_01_100000_create_widgets_table.go"), []byte(createWidget), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "2026_04_01_100001_alter_widgets_table.go"), []byte(alterWidget), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "exported")
	if _, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260401100001_alter_widgets_table.up.sql"), `ALTER TABLE "widgets" RENAME COLUMN "name" TO "label"`)
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260401100001_alter_widgets_table.up.sql"), `ALTER TABLE "widgets" ADD COLUMN "slug" VARCHAR(255)`)
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260320100000_create_users_table.up.sql"), `ON "users" ("email_encrypted")`)
	assertFileNotContains(t, filepath.Join(out, "database", "migrations", "20260320100000_create_users_table.up.sql"), `ON "users" ("email")`)
	assertCleanExportReport(t, out)
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")

	behaviorTest := `package migrations

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedAlterMigrationsRunAndRollback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(db, "sqlite")
	if err := runner.Migrate(Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cols := columnSet(t, db, "widgets")
	for _, want := range []string{"id", "label", "slug"} {
		if !cols[want] {
			t.Fatalf("widgets missing column %q after migrate: %#v", want, cols)
		}
	}
	if cols["name"] {
		t.Fatalf("widgets retained renamed column name after migrate: %#v", cols)
	}
	if err := runner.Rollback(Registry); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if db.Migrator().HasTable("widgets") {
		t.Fatalf("widgets table should be removed after full batch rollback")
	}
}

func columnSet(t *testing.T, db *gorm.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "migrations", "exported_alter_behavior_test.go"), []byte(behaviorTest), 0o644); err != nil {
		t.Fatal(err)
	}
	runExported(t, out, "go", "test", "./database/migrations")
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

func TestFindingCategoryClassifiesUnlowerableBoundariesAsUnsupported(t *testing.T) {
	for _, rule := range []string{
		"action_export_unsupported_signature",
		"action_export_unsupported_query",
		"gate_export_unsupported_signature",
		"gate_export_policy_dependency",
		"gate_export_dynamic_role",
		"gate_export_callsite",
	} {
		if got := findingCategory(rule); got != "unsupported" {
			t.Fatalf("findingCategory(%q) = %q, want unsupported", rule, got)
		}
	}
	for _, rule := range []string{
		"actions_audit",
		"raw_sql_migration",
	} {
		if got := findingCategory(rule); got != "manual" {
			t.Fatalf("findingCategory(%q) = %q, want manual", rule, got)
		}
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
		`q = q.Order(models.OrderClause("id", "ASC"))`,
		"return func() ([]models.User, error)",
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("rewritten source missing %q:\n%s", want, got)
		}
	}
}

func TestRewriteAssignedMutableQueryVariable(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Index(role string, limit int) ([]models.User, error) {
	q := models.QueryUser()
	q = q.WhereRole(role)
	q = q.OrderByID("ASC")
	q = q.Limit(limit)
	q = q.SelectAll()
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
		`q = q.Order(models.OrderClause("id", "ASC"))`,
		"q = q.Limit(limit)",
		"return func() ([]models.User, error)",
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("rewritten source missing %q:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{"WhereRole", "OrderByID", "SelectAll"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source still contains %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteDirectQueryOrderByMethods(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Index(role string, direction string) ([]models.User, error) {
	return models.QueryUser().
		WhereRole(role).
		OrderByID(direction).
		OrderBy("email", "DESC").
		Limit(10).
		All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`models.DB.Model(&models.User{})`,
		`Where("role = ?", role)`,
		`Order(models.OrderClause("id", direction))`,
		`models.OrderClause("email", "DESC")`,
		`Limit(10)`,
	)
	for _, unexpected := range []string{"OrderByID", `Order("id" + " "`, `Order("email" + " "`} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained unsafe/order query method %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteQueryEncryptedFiltersUseStorageScopes(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
		schemaTables: map[string]*schema.Table{
			"User": {Name: "users", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "email", Type: schema.String, IsEncrypted: true},
				{Name: "private_key", Type: schema.String, IsSealed: true},
			}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Find(email string) (*models.User, error) {
	return models.QueryUser().
		WhereEmail(email).
		OrderByEmail("ASC").
		First()
}

func Denied(privateKey string) (*models.User, error) {
	return models.QueryUser().WherePrivateKey(privateKey).First()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`EncryptedWhereScope("email_encrypted", "=", email`,
		`Order(models.OrderClause("email_encrypted", "ASC"))`,
		`UnsupportedQueryFilterScope("sealed column private_key cannot be filtered"`,
	)
	for _, unexpected := range []string{`Where("email = ?"`, `Where("private_key = ?"`, "WhereEmail", "WherePrivateKey"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained unsafe encrypted query %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableQueryEncryptedFiltersUseStorageScopes(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
		schemaTables: map[string]*schema.Table{
			"User": {Name: "users", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "email", Type: schema.String, IsEncrypted: true},
			}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Find(email string) ([]models.User, error) {
	q := models.QueryUser()
	q.WhereEmail(email)
	return q.All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`q = q.Scopes(models.EncryptedWhereScope("email_encrypted", "=", email))`,
		`return func() ([]models.User, error)`,
	)
	if strings.Contains(got, `Where("email = ?"`) || strings.Contains(got, "WhereEmail") {
		t.Fatalf("rewritten mutable source retained unsafe encrypted query:\n%s", got)
	}
}

func TestRewriteQueryFilterOperatorSuffixes(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Post": true},
	}
	src := []byte(`package controllers

import (
	"time"

	"example.com/app/app/models"
)

func Search(term string, excluded string, after time.Time, before time.Time) ([]models.Post, error) {
	return models.QueryPost().
		WhereTitleLike(term).
		WhereBodyNotLike(excluded).
		WhereCreatedAtAfter(after).
		WhereUpdatedAtBefore(before).
		All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	for _, want := range []string{
		`Where("title LIKE ?", term)`,
		`Where("body NOT LIKE ?", excluded)`,
		`Where("created_at > ?", after)`,
		`Where("updated_at < ?", before)`,
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("rewritten source missing %q:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{"WhereTitleLike", "WhereBodyNotLike", "WhereCreatedAtAfter", "WhereUpdatedAtBefore"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source still contains %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableQueryFilterOperatorSuffixes(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Post": true},
	}
	src := []byte(`package controllers

import (
	"time"

	"example.com/app/app/models"
)

func Search(term string, after time.Time) ([]models.Post, error) {
	q := models.QueryPost()
	q.WhereTitleLike(term)
	q.WhereCreatedAtAfter(after)
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
		`q = q.Where("title LIKE ?", term`,
		`q = q.Where("created_at > ?", after`,
		"return func() ([]models.Post, error)",
	} {
		if !strings.Contains(compact, want) {
			t.Fatalf("rewritten source missing %q:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{"WhereTitleLike", "WhereCreatedAtAfter"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source still contains %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteQueryBetweenFilter(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Post": true},
	}
	src := []byte(`package controllers

import (
	"time"

	"example.com/app/app/models"
)

func Search(start time.Time, end time.Time) ([]models.Post, error) {
	return models.QueryPost().
		WhereCreatedAtBetween(start, end).
		All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(compact, `Where("created_at BETWEEN ? AND ?", start, end)`) {
		t.Fatalf("rewritten source missing BETWEEN filter:\n%s", got)
	}
	if strings.Contains(got, "WhereCreatedAtBetween") {
		t.Fatalf("rewritten source still contains WhereCreatedAtBetween:\n%s", got)
	}
}

func TestRewriteMutableQueryBetweenFilter(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Post": true},
	}
	src := []byte(`package controllers

import (
	"time"

	"example.com/app/app/models"
)

func Search(start time.Time, end time.Time) ([]models.Post, error) {
	q := models.QueryPost()
	q.WhereCreatedAtBetween(start, end)
	return q.All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(compact, `q = q.Where("created_at BETWEEN ? AND ?", start, end`) {
		t.Fatalf("rewritten source missing mutable BETWEEN filter:\n%s", got)
	}
	if strings.Contains(got, "WhereCreatedAtBetween") {
		t.Fatalf("rewritten source still contains WhereCreatedAtBetween:\n%s", got)
	}
}

func TestRewriteImmutableQueryAllVersions(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Transaction": true},
		integrityModels: map[string]integrityModelInfo{
			"Transaction": {
				Table:     &schema.Table{Name: "transactions"},
				Immutable: true,
			},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func History(accountID string) ([]models.Transaction, error) {
	return models.QueryTransaction().
		WhereAccountID(accountID).
		AllVersions().
		All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(compact, `Where("account_id = ?", accountID)`) {
		t.Fatalf("rewritten source missing account filter:\n%s", got)
	}
	if strings.Contains(got, "Where(\"version_id = (SELECT MAX(version_id)") {
		t.Fatalf("AllVersions query retained latest-version predicate:\n%s", got)
	}
	if strings.Contains(got, "AllVersions") || strings.Contains(got, "QueryTransaction") {
		t.Fatalf("rewritten source retained Pickle query method:\n%s", got)
	}
}

func TestRewriteMutableImmutableAllVersions(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Transaction": true},
		integrityModels: map[string]integrityModelInfo{
			"Transaction": {
				Table:     &schema.Table{Name: "transactions"},
				Immutable: true,
			},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func History() ([]models.Transaction, error) {
	q := models.QueryTransaction()
	q.WhereAccountID("acct-1")
	q.AllVersions()
	return q.All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(compact, `q = q.Where("account_id = ?", "acct-1"`) {
		t.Fatalf("rewritten source missing mutable account filter:\n%s", got)
	}
	if strings.Contains(got, "version_id = (SELECT MAX(version_id)") {
		t.Fatalf("mutable AllVersions query retained latest-version predicate:\n%s", got)
	}
	if strings.Contains(got, "AllVersions") || strings.Contains(got, "QueryTransaction") {
		t.Fatalf("rewritten source retained Pickle query method:\n%s", got)
	}
}

func TestRewriteMutableImmutableQueryDefersLatestPredicateUntilTerminal(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Transaction": true},
		integrityModels: map[string]integrityModelInfo{
			"Transaction": {
				Table:     &schema.Table{Name: "transactions"},
				Immutable: true,
			},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Current(accountID string) ([]models.Transaction, error) {
	q := models.QueryTransaction()
	q.WhereAccountID(accountID)
	return q.All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`q := models.DB.Model(&models.`,
		`Transaction{})`,
		`q = q.Where("account_id = ?", accountID`,
		`version_id = (SELECT MAX(version_id) FROM transactions latest WHERE latest.id = transactions.id)`,
	)
	if strings.Contains(got, "QueryTransaction") {
		t.Fatalf("rewritten source retained Pickle query method:\n%s", got)
	}
}

func TestRewriteQueryLockClauses(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import (
	"time"

	"example.com/app/app/models"
)

func Claim(status string) error {
	return models.WithTransaction(func(tx *models.Tx) error {
		jobs, err := tx.QueryJob().
			WhereStatus(status).
			Lock().
			SkipLocked().
			Timeout(time.Second).
			Limit(10).
			All()
		_ = jobs
		return err
	})
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"gorm.io/gorm/clause"`,
		`models.WithTransaction(func(tx *models.Tx) error`,
		`models.ApplyLockTimeout(tx.DB, time.Second)`,
		`tx.DB.Model(&models.`,
		`Job{})`,
		`Clauses(clause.`,
		`Locking{Strength: "UPDATE", Options: "SKIP LOCKED"`,
		`Where("status = ?", status)`,
		"Limit(10)",
	)
	for _, unexpected := range []string{"QueryJob", ".Lock()", ".SkipLocked()"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteQueryShareNoWaitLockClause(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func ReadOne(id string) error {
	return models.WithTransaction(func(tx *models.Tx) error {
		job, err := tx.QueryJob().
			WhereID(id).
			LockForShare().
			NoWait().
			First()
		_ = job
		return err
	})
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"gorm.io/gorm/clause"`,
		`tx.DB.Model(&models.`,
		`Job{})`,
		`Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"`,
		`Where("id = ?", id)`,
	)
	for _, unexpected := range []string{"QueryJob", "LockForShare", "NoWait"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteQueryWithDescriptiveTransactionName(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Claim(status string) error {
	return models.WithTransaction(func(transaction *models.Tx) error {
		jobs, err := transaction.QueryJob().
			WhereStatus(status).
			Lock().
			All()
		_ = jobs
		return err
	})
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"gorm.io/gorm/clause"`,
		`models.WithTransaction(func(transaction *models.Tx) error`,
		`transaction.DB.Model(&models.`,
		`Job{})`,
		`Clauses(clause.Locking{Strength: "UPDATE"`,
		`Where("status = ?", status)`,
	)
	for _, unexpected := range []string{"QueryJob", ".Lock()"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteQueryLockOutsideTransactionReturnsPickleEquivalentError(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Claim(status string) ([]models.Job, error) {
	return models.QueryJob().
		WhereStatus(status).
		Lock().
		All()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`return nil, models.NewLockOutsideTransactionError("Job")`,
	)
	if strings.Contains(got, "clause.Locking") {
		t.Fatalf("outside-transaction lock should not emit a GORM lock clause:\n%s", got)
	}
}

func TestRewriteMutableQueryLockClauses(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Claim() error {
	return models.WithTransaction(func(tx *models.Tx) error {
		q := tx.QueryJob()
		q.Lock()
		q.SkipLocked()
		jobs, err := q.All()
		_ = jobs
		return err
	})
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"gorm.io/gorm/clause"`,
		`q := tx.DB.Model(&models.`,
		`Job{})`,
		`q = q.Clauses(clause.Locking{Strength: "UPDATE"`,
		`q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"`,
	)
	for _, unexpected := range []string{"QueryJob", ".Lock()", ".SkipLocked()"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableQueryShareNoWaitLockClauses(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func ReadOne(id string) (*models.Job, error) {
	q := models.QueryJob()
	q.WhereID(id)
	q.LockForShare()
	q.NoWait()
	return q.First()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"gorm.io/gorm/clause"`,
		`q = q.Where("id = ?", id`,
		`q = q.Clauses(clause.Locking{Strength: "SHARE"`,
		`q = q.Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"`,
	)
	for _, unexpected := range []string{"QueryJob", "LockForShare", "NoWait"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableQueryTimeoutInTransaction(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
	}
	src := []byte(`package controllers

import (
	"time"

	"example.com/app/app/models"
)

func Claim() ([]models.Job, error) {
	var jobs []models.Job
	err := models.WithTransaction(func(tx *models.Tx) error {
	q := tx.QueryJob()
	q.Lock()
	q.Timeout(time.Second)
	var err error
	jobs, err = q.All()
	return err
	})
	return jobs, err
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"gorm.io/gorm/clause"`,
		`models.ApplyLockTimeout(tx.DB, time.Second)`,
		`q = q.Clauses(clause.Locking{Strength: "UPDATE"`,
	)
	for _, unexpected := range []string{"QueryJob", ".Timeout("} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteAliasedModelsImportQueryChain(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
	}
	src := []byte(`package controllers

import m "example.com/app/app/models"

func Show(id string) (*m.User, error) {
	return m.QueryUser().WhereID(id).First()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	if strings.Contains(got, `m "exported-app/app/models"`) {
		t.Fatalf("models import kept source alias:\n%s", got)
	}
	compact := strings.Join(strings.Fields(got), " ")
	assertContainsAll(t, compact,
		`"exported-app/app/models"`,
		"func Show(id string) (*models.User, error)",
		"models.DB.Model(&models.User{})",
		`.Where("id = ?", id).First(&record).Error`,
	)
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

func assertContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("rewritten source missing %q:\n%s", want, got)
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

func assertCleanExportReport(t *testing.T, out string) {
	t.Helper()
	reportPath := filepath.Join(out, "EXPORT_REPORT.md")
	assertFileContains(t, reportPath, "## Unsupported\n\nNo unsupported export findings.")
	assertFileNotContains(t, reportPath, "## Partial Support")
	assertFileNotContains(t, reportPath, "## Omitted")
	assertFileNotContains(t, reportPath, "## Manual Review")
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
