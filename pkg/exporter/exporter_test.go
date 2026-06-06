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
	writeTestCommand(t, projectDir)
	writeVisibilityQuerySourceFixture(t, projectDir)
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
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "gorm.io/gorm/logger")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "Logger: logger.Default.LogMode(logger.Silent)")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func sanitizedDatabaseStartupError")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), `log.Fatal(sanitizedDatabaseStartupError("config"))`)
	assertFileNotContains(t, filepath.Join(out, "config", "support.go"), "failed to open database: %v")
	assertFileNotContains(t, filepath.Join(out, "config", "support.go"), "failed to initialize database: %v")
	assertFileNotContains(t, filepath.Join(out, "config", "support.go"), "log.Fatal(err)")
	assertFileContains(t, filepath.Join(out, "config", "app.go"), "func app() AppConfig")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func BuiltinCommands() []Command")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "AuditMarkerCommand{}")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func HTTPHandler() http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), `mux.HandleFunc("/", exportedNotFound)`)
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "func exportedNotFound")
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
	assertFileContains(t, filepath.Join(out, "database", "migrations", "support.go"), `return fmt.Errorf("fresh rollback %s: %w", entries[i].ID, err)`)
	assertFileNotContains(t, filepath.Join(out, "database", "migrations", "support.go"), `_ = r.execMigrationFile(entries[i].DownFile)`)
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
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), `errors.New("oauth: database error")`)
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), `errors.New("oauth: token generation error")`)
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "failed to store token")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "oauth", "oauth.go"), "return ctx.Error(err)")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func CSRF")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "io.ReadFull(csrfNonceReader, nonce)")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), `panic("csrf: failed to generate random nonce")`)
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), `panic("csrf: failed to generate random nonce: " + err.Error())`)
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), `errors.New("jwt: database error")`)
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), `return "", errors.New("jwt: database error")`)
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), `errors.New("jwt: unsupported algorithm")`)
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "jwt: unsupported algorithm %s")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "jwt: revoke token: %w")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "jwt: revoke all for user: %w")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "len(parts[0]) != 64 || len(parts[1]) != 64")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func validSessionID")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "func validCookieName")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "maxSessionTTLSeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "boundedPositiveSeconds")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), `errors.New("session: invalid session value")`)
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), `errors.New("session: database not configured")`)
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "session: put: %w")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "auth", "session", "session.go"), "session: get: %w")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_ban.go"), "DB.Save(user).Error")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_promote.go"), "type PromoteResult struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_standalone_gate.go"), "func CanView")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_ban_gate_gen.go"), `HasAnyRole("admin")`)
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func (m *User) Ban")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func (m *User) Promote")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func AuthorizeBan")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func AuthorizeView")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "AuthorizeBan(ctx, m)")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "roleID, err := AuthorizeBan(ctx, m)")
	assertFileContains(t, filepath.Join(out, "app", "models", "action_audit_support.go"), "func runAuditedAction")
	assertFileContains(t, filepath.Join(out, "app", "models", "action_audit_support.go"), "func auditDatabaseError() error")
	assertFileContains(t, filepath.Join(out, "app", "models", "action_audit_support.go"), "var errAuditUserID = errors.New(\"audit user id\")")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "action_audit_support.go"), "audit user id: %w")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "action_audit_support.go"), "return db.Exec(")
	assertFileContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "func LoadRoles")
	assertFileContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "func RequireRole")
	assertFileContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "errRoleDatabase")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "return ctx.Error(err)")
	assertFileContains(t, filepath.Join(out, "database", "policies", "support.go"), `return fmt.Errorf("policy fresh drop %s", table)`)
	assertFileNotContains(t, filepath.Join(out, "database", "policies", "support.go"), `_ = db.Exec("DROP TABLE IF EXISTS roles").Error`)
	assertFileContains(t, filepath.Join(out, "app", "services", "action_call.go"), "models.BanAction")
	assertFileContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `Select([]string{`)
	assertFileContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `"email_encrypted"`)
	assertFileContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `"body"`)
	assertFileContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `RoleVisibilitySelectScope`)
	assertFileContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `"editor"`)
	assertFileContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `"admin"`)
	assertFileNotContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), `"password_hash_encrypted"`)
	assertFileNotContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), "SelectPublic")
	assertFileNotContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), "SelectOwner")
	assertFileNotContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), "SelectForRoles")
	assertFileNotContains(t, filepath.Join(out, "app", "services", "visibility_selectors.go"), "SelectForOwner")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "models.DB.Model(&models.User{})")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "QueryUser")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "basic-crud/internal/httpx")
	assertFileContains(t, filepath.Join(out, "internal", "httpx", "httpx.go"), "func writeRouterNotFound")
	assertFileNotContains(t, filepath.Join(out, "internal", "httpx", "httpx.go"), "pickle:")
	assertFileNotContains(t, filepath.Join(out, "internal", "httpx", "httpx.go"), "pickle export:")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "middleware", "rbac_support.go"), "pickle export:")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Target ORM: `gorm`")

	assertStandaloneNoPickleRuntime(t, out)
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

func TestExportPreservesCustomRouteVars(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	routePath := filepath.Join(projectDir, "routes", "web.go")
	data, err := os.ReadFile(routePath)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(data), "var API = pickle.Routes", "var Web = pickle.Routes", 1)
	if rewritten == string(data) {
		t.Fatal("route fixture did not contain API route var")
	}
	if err := os.WriteFile(routePath, []byte(rewritten), 0o644); err != nil {
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

	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.Web.RegisterRoutes(mux)")
	assertFileNotContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
	assertStandaloneNoPickleRuntime(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedConfigBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package config

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestConnectionConfigRejectsUnsupportedDriversWithoutPanic(t *testing.T) {
	conn := ConnectionConfig{Driver: "oracle-password=swordfish", Name: "ignored"}
	if err := conn.Validate(); err == nil || err.Error() != "unsupported database driver" {
		t.Fatalf("Validate() error = %v, want unsupported driver", err)
	} else if leaksSecret(err.Error()) {
		t.Fatalf("Validate() leaked detail: %v", err)
	}
	if got := conn.DSN(); got != "" {
		t.Fatalf("unsupported DSN = %q, want empty string", got)
	}
	if _, err := TryOpenDB(conn); err == nil || err.Error() != "unsupported database driver" {
		t.Fatalf("TryOpenDB() error = %v, want unsupported driver", err)
	} else if leaksSecret(err.Error()) {
		t.Fatalf("TryOpenDB() leaked detail: %v", err)
	}
	if _, err := TryOpenGORM(conn); err == nil || err.Error() != "unsupported database driver" {
		t.Fatalf("TryOpenGORM() error = %v, want unsupported driver", err)
	} else if leaksSecret(err.Error()) {
		t.Fatalf("TryOpenGORM() leaked detail: %v", err)
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

func TestConnectionConfigTryOpenGORMSilencesDefaultLogger(t *testing.T) {
	if os.Getenv("PICKLE_EXPORT_GORM_LOG_CHILD") == "1" {
		db, err := TryOpenGORM(ConnectionConfig{Driver: "sqlite", Name: ":memory:"})
		if err != nil {
			t.Fatalf("TryOpenGORM(sqlite): %v", err)
		}
		_ = db.Exec(` + "`" + `SELECT * FROM missing_table WHERE secret = ?` + "`" + `, "password=swordfish").Error
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestConnectionConfigTryOpenGORMSilencesDefaultLogger$")
	cmd.Env = append(os.Environ(), "PICKLE_EXPORT_GORM_LOG_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child test failed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "swordfish") || strings.Contains(string(out), "password") || strings.Contains(string(out), "missing_table") {
		t.Fatalf("default GORM logger leaked query detail: %s", out)
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

func leaksSecret(value string) bool {
	return strings.Contains(value, "swordfish") || strings.Contains(value, "password") || strings.Contains(value, "oracle")
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
	"errors"
	"strings"
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

func TestTransactionsFailClosedOnNilInputs(t *testing.T) {
	previousDB := DB
	defer func() { DB = previousDB }()

	SetDB(nil)
	if err := WithTransaction(func(tx *Tx) error { return nil }); err == nil || err.Error() != "models: DB is nil" {
		t.Fatalf("WithTransaction nil DB error = %v, want sanitized DB error", err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(db)
	if err := WithTransaction(nil); err == nil || err.Error() != "models: transaction callback is nil" {
		t.Fatalf("WithTransaction nil callback error = %v, want sanitized callback error", err)
	}

	var nilTx *Tx
	if err := nilTx.Transaction(func(tx *Tx) error { return nil }); err == nil || err.Error() != "models: transaction is nil" {
		t.Fatalf("nil Tx Transaction error = %v, want sanitized transaction error", err)
	}
	if err := (&Tx{}).Transaction(func(tx *Tx) error { return nil }); err == nil || err.Error() != "models: transaction is nil" {
		t.Fatalf("empty Tx Transaction error = %v, want sanitized transaction error", err)
	}
	if err := (&Tx{DB: db}).Transaction(nil); err == nil || err.Error() != "models: transaction callback is nil" {
		t.Fatalf("Tx nil callback error = %v, want sanitized callback error", err)
	}
}

func TestWithTransactionCommitsAndRollsBack(t *testing.T) {
	previousDB := DB
	defer func() { DB = previousDB }()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(db)
	if err := db.Exec("CREATE TABLE tx_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}

	if err := WithTransaction(func(tx *Tx) error {
		return tx.DB.Exec("INSERT INTO tx_items (name) VALUES (?)", "committed").Error
	}); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	if got := countTxItems(t, db); got != 1 {
		t.Fatalf("rows after committed transaction = %d, want 1", got)
	}

	errRollback := errors.New("rollback")
	if err := WithTransaction(func(tx *Tx) error {
		if err := tx.DB.Exec("INSERT INTO tx_items (name) VALUES (?)", "rolled-back").Error; err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("rollback transaction error = %v, want rollback sentinel", err)
	}
	if got := countTxItems(t, db); got != 1 {
		t.Fatalf("rows after rolled-back transaction = %d, want 1", got)
	}

	if err := WithTransaction(func(tx *Tx) error {
		return tx.Transaction(func(nested *Tx) error {
			return nested.DB.Exec("INSERT INTO tx_items (name) VALUES (?)", "nested").Error
		})
	}); err != nil {
		t.Fatalf("nested transaction: %v", err)
	}
	if got := countTxItems(t, db); got != 2 {
		t.Fatalf("rows after nested committed transaction = %d, want 2", got)
	}
}

func countTxItems(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Table("tx_items").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func TestOrderClauseFailsClosedOnUnsafeInput(t *testing.T) {
	if got := OrderClause("created_at", " desc "); got != "created_at DESC" {
		t.Fatalf("OrderClause safe result = %q, want created_at DESC", got)
	}
	for _, tc := range []struct {
		name      string
		column    string
		direction string
	}{
		{name: "unsafe column", column: "created_at; DROP TABLE users", direction: "ASC"},
		{name: "unsafe direction", column: "created_at", direction: "DESC; DROP TABLE users"},
		{name: "empty column", column: "", direction: "ASC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := OrderClause(tc.column, tc.direction); got != "" {
				t.Fatalf("OrderClause(%q, %q) = %q, want empty fail-closed clause", tc.column, tc.direction, got)
			}
		})
	}
}

func TestOrderClauseUnsafeInputDoesNotReachSQL(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	stmt := db.Session(&gorm.Session{DryRun: true}).
		Model(&User{}).
		Order(OrderClause("created_at; DROP TABLE users", "DESC; DROP TABLE users")).
		Find(&[]User{}).Statement
	sql := stmt.SQL.String()
	if strings.Contains(sql, "DROP TABLE") || strings.Contains(sql, "DESC;") {
		t.Fatalf("unsafe order input leaked into SQL: %s", sql)
	}
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
	if _, err := jwtDriver.Authenticate(nil); err == nil || err.Error() != "missing authorization header" {
		t.Fatalf("nil jwt request error = %v, want missing authorization header", err)
	}
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
	badAlgJWTDriver := jwt.NewDriver(func(key, fallback string) string {
		switch key {
		case "JWT_SECRET":
			return "0123456789abcdef0123456789abcdef"
		case "JWT_ALGORITHM":
			return "HS256-password=swordfish"
		default:
			return fallback
		}
	}, db, "sqlite")
	if _, err := badAlgJWTDriver.SignToken(jwt.Claims{Subject: "secret-user"}); err == nil || err.Error() != "jwt: unsupported algorithm" {
		t.Fatalf("unsupported jwt algorithm error = %v, want sanitized unsupported algorithm", err)
	} else if strings.Contains(err.Error(), "swordfish") || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "HS256-") {
		t.Fatalf("unsupported jwt algorithm leaked detail: %v", err)
	}
	jwtRowsBeforeOversizedClaims := countSQLRows(t, db, "jwt_tokens")
	for _, tc := range []struct {
		name   string
		claims jwt.Claims
		leak   string
	}{
		{name: "subject", claims: jwt.Claims{Subject: strings.Repeat("subject-secret", 80)}, leak: "subject-secret"},
		{name: "role", claims: jwt.Claims{Subject: "user-oversized-role", Role: strings.Repeat("role-secret", 80)}, leak: "role-secret"},
		{name: "jti", claims: jwt.Claims{Subject: "user-oversized-jti", JTI: strings.Repeat("jti-secret", 80)}, leak: "jti-secret"},
	} {
		t.Run("oversized jwt claim "+tc.name, func(t *testing.T) {
			token, err := jwtDriver.SignToken(tc.claims)
			if err == nil || err.Error() != "jwt: invalid claims" {
				t.Fatalf("SignToken oversized %s token=%q err=%v, want sanitized invalid claims", tc.name, token, err)
			}
			if token != "" {
				t.Fatalf("SignToken oversized %s returned token length %d", tc.name, len(token))
			}
			if strings.Contains(err.Error(), tc.leak) || strings.Contains(err.Error(), "database") || strings.Contains(err.Error(), "jwt_tokens") {
				t.Fatalf("SignToken oversized %s leaked detail: %v", tc.name, err)
			}
		})
	}
	if rows := countSQLRows(t, db, "jwt_tokens"); rows != jwtRowsBeforeOversizedClaims {
		t.Fatalf("oversized jwt claims inserted %d token rows, want %d", rows, jwtRowsBeforeOversizedClaims)
	}

	closedJWTDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := closedJWTDB.Close(); err != nil {
		t.Fatal(err)
	}
	closedJWTDriver := jwt.NewDriver(env, closedJWTDB, "sqlite")
	if _, err := closedJWTDriver.SignToken(jwt.Claims{Subject: "secret-user"}); err == nil || err.Error() != "jwt: database error" || strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "secret-user") {
		t.Fatalf("closed DB sign token error = %v", err)
	}
	if err := closedJWTDriver.RevokeToken("secret-jti"); err == nil || err.Error() != "jwt: database error" || strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "secret-jti") {
		t.Fatalf("closed DB revoke token error = %v", err)
	}
	if err := closedJWTDriver.RevokeAllForUser("secret-user"); err == nil || err.Error() != "jwt: database error" || strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "secret-user") {
		t.Fatalf("closed DB revoke all error = %v", err)
	}
	validateJWTDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateJWTDB.Exec(` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	validateJWTDriver := jwt.NewDriver(env, validateJWTDB, "sqlite")
	validateJWT, err := validateJWTDriver.SignToken(jwt.Claims{Subject: "secret-user", Role: "admin"})
	if err != nil {
		t.Fatalf("sign validation jwt: %v", err)
	}
	if err := validateJWTDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := validateJWTDriver.ValidateToken(validateJWT); err == nil || !errors.Is(err, jwt.ErrInvalidToken) {
		t.Fatalf("closed DB validate token error = %v, want ErrInvalidToken", err)
	} else if strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "secret-user") || strings.Contains(err.Error(), "jwt_tokens") {
		t.Fatalf("closed DB validate token leaked detail: %v", err)
	}

	oauthDriver := auth.Driver("oauth").(*oauth.Driver)
	if _, err := oauthDriver.Authenticate(nil); err == nil || err.Error() != "missing bearer token" {
		t.Fatalf("nil oauth request error = %v, want missing bearer token", err)
	}
	nilTokenResp := oauthDriver.TokenEndpoint(httpx.NewContext(nil))
	if nilTokenResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("nil oauth token request status = %d body = %#v", nilTokenResp.StatusCode, nilTokenResp.Body)
	}
	if body, ok := nilTokenResp.Body.(map[string]string); !ok || body["error"] != "invalid_request" || strings.Contains(fmt.Sprint(body), "panic") {
		t.Fatalf("nil oauth token request body = %#v", nilTokenResp.Body)
	}
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
	oauthReq, _ := http.NewRequest("GET", "/", nil)
	oauthReq.Header.Set("Authorization", "Bearer opaque")
	oauthAuthInfo, err := oauthDriver.Authenticate(oauthReq)
	if err != nil {
		t.Fatalf("authenticate oauth bearer token: %v", err)
	}
	if oauthAuthInfo.UserID != "client-1" || oauthAuthInfo.Role != "client" {
		t.Fatalf("oauth bearer auth info = %#v", oauthAuthInfo)
	}
	lowercaseBearerReq, _ := http.NewRequest("GET", "/", nil)
	lowercaseBearerReq.Header.Set("Authorization", "bearer   opaque")
	if _, err := oauthDriver.Authenticate(lowercaseBearerReq); err != nil {
		t.Fatalf("lowercase oauth bearer token should authenticate: %v", err)
	}
	for _, header := range []string{
		"Bearer",
		"Bearer opaque extra-secret",
		"Bearer\t",
		"Basic " + strings.Repeat("x", 128),
		"Bearer " + strings.Repeat("x", 13<<10),
	} {
		malformedReq, _ := http.NewRequest("GET", "/", nil)
		malformedReq.Header.Set("Authorization", header)
		if _, err := oauthDriver.Authenticate(malformedReq); err == nil || err.Error() != "missing bearer token" {
			t.Fatalf("malformed oauth bearer header %q error = %v, want missing bearer token", header, err)
		} else if strings.Contains(err.Error(), "extra-secret") || strings.Contains(err.Error(), strings.Repeat("x", 128)) {
			t.Fatalf("malformed oauth bearer header leaked detail: %v", err)
		}
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
	closedOAuthDB, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closedOAuthDB.Exec(` + "`" + `CREATE TABLE oauth_tokens (token TEXT PRIMARY KEY, client_id TEXT NOT NULL, expires_at DATETIME NOT NULL, created_at DATETIME NOT NULL)` + "`" + `); err != nil {
		t.Fatal(err)
	}
	if err := closedOAuthDB.Close(); err != nil {
		t.Fatal(err)
	}
	closedOAuthDriver := oauth.NewDriver(func(key, fallback string) string {
		switch key {
		case "OAUTH_CLIENT_ID":
			return "client-1"
		case "OAUTH_CLIENT_SECRET":
			return "secret-1"
		default:
			return fallback
		}
	}, closedOAuthDB, "sqlite")
	closedTokenReq, _ := http.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	closedTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	closedTokenReq.SetBasicAuth("client-1", "secret-1")
	closedTokenResp := closedOAuthDriver.TokenEndpoint(httpx.NewContext(closedTokenReq))
	if closedTokenResp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("closed oauth DB status = %d body = %#v", closedTokenResp.StatusCode, closedTokenResp.Body)
	}
	if strings.Contains(fmt.Sprint(closedTokenResp.Body), "sql:") || strings.Contains(fmt.Sprint(closedTokenResp.Body), "closed") {
		t.Fatalf("closed oauth DB response leaked detail: %#v", closedTokenResp.Body)
	}
	if _, err := closedOAuthDriver.ValidateToken("secret-oauth-token"); err == nil || err.Error() != "oauth: database error" {
		t.Fatalf("closed oauth ValidateToken error = %v, want sanitized database error", err)
	} else if strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "secret-oauth-token") || strings.Contains(err.Error(), "oauth_tokens") {
		t.Fatalf("closed oauth ValidateToken leaked detail: %v", err)
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
	if _, err := sessionDriver.Authenticate(nil); err == nil || err.Error() != "session: missing session cookie" {
		t.Fatalf("nil session request error = %v, want missing session cookie", err)
	}
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
	nilCtxResp := auth.DefaultAuthMiddleware(nil, func() httpx.Response {
		t.Fatal("middleware should not call next for nil context")
		return httpx.Response{}
	})
	if nilCtxResp.Status != http.StatusUnauthorized {
		t.Fatalf("nil context auth middleware status = %d, want 401", nilCtxResp.Status)
	}
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

	logs.Reset()
	nilNextReq, _ := http.NewRequest("GET", "/", nil)
	nilNextReq.Header.Set("Authorization", "Bearer "+token)
	nilNextCtx := httpx.NewContext(nilNextReq)
	nilNextResp := auth.DefaultAuthMiddleware(nilNextCtx, nil)
	if nilNextResp.Status != http.StatusInternalServerError {
		t.Fatalf("nil next middleware status = %d, want 500", nilNextResp.Status)
	}
	if body := fmt.Sprint(nilNextResp.Body); strings.Contains(body, "user-4") || strings.Contains(body, token) || strings.Contains(body, "panic") {
		t.Fatalf("nil next middleware response leaked detail: %#v", nilNextResp.Body)
	}
	if strings.Contains(logs.String(), "user-4") || strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "panic") {
		t.Fatalf("nil next middleware log leaked detail: %s", logs.String())
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
	assertPanicsExactly(t, "auth: driver unavailable", func() {
		auth.Driver("jwt")
	})
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

func assertPanicsExactly(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("expected panic %q", want)
		}
		if fmt.Sprint(got) != want {
			t.Fatalf("panic = %q, want %q", got, want)
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

func countSQLRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
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
	var nilErr *BindingError
	if got := nilErr.Error(); got != "binding failed" {
		t.Fatalf("nil BindingError.Error() = %q, want binding failed", got)
	}
	if got := (&BindingError{}).Error(); got != "binding failed" {
		t.Fatalf("empty BindingError.Error() = %q, want binding failed", got)
	}

	validReq := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `)
	if req, bindErr := BindCreateUserRequest(validReq); bindErr != nil {
		t.Fatalf("valid bind error = %v", bindErr)
	} else if req.Name != "Ada" || req.Email != "ada@example.com" {
		t.Fatalf("valid request = %#v", req)
	}

	charsetReq := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `)
	charsetReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	if _, bindErr := BindCreateUserRequest(charsetReq); bindErr != nil {
		t.Fatalf("charset JSON bind error = %v", bindErr)
	}

	missingContentType := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `)
	missingContentType.Header.Del("Content-Type")
	if _, bindErr := BindCreateUserRequest(missingContentType); bindErr == nil || bindErr.Status != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content type bind error = %#v, want 415", bindErr)
	} else if strings.Contains(bindErr.Error(), "application/jsonevil") || !strings.Contains(bindErr.Error(), "application/json") {
		t.Fatalf("missing content type error = %q, want sanitized json requirement", bindErr.Error())
	}

	spoofedContentType := requestWithBody(` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `)
	spoofedContentType.Header.Set("Content-Type", "application/jsonevil")
	if _, bindErr := BindCreateUserRequest(spoofedContentType); bindErr == nil || bindErr.Status != http.StatusUnsupportedMediaType {
		t.Fatalf("spoofed content type bind error = %#v, want 415", bindErr)
	} else if strings.Contains(bindErr.Error(), "application/jsonevil") {
		t.Fatalf("spoofed content type error leaked raw header: %q", bindErr.Error())
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
	} else {
		body := bindErr.Error()
		if !strings.Contains(body, "email:") || !strings.Contains(body, "password:") {
			t.Fatalf("validation bind error should use JSON field names: %q", body)
		}
		if strings.Contains(body, "Email:") || strings.Contains(body, "Password:") {
			t.Fatalf("validation bind error leaked Go field names: %q", body)
		}
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

func TestExportedCSRFFailsClosedForNilRequest(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, db, "sqlite")
	resp := CSRF(httpx.NewContext(nil), func() httpx.Response {
		t.Fatal("CSRF should not call next for nil request")
		return httpx.Response{}
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("nil request CSRF status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	body, ok := resp.Body.(map[string]string)
	if !ok || body["error"] != "CSRF request missing" {
		t.Fatalf("nil request CSRF body = %#v", resp.Body)
	}
	if got := sessionIDFromRequest(nil); got != "" {
		t.Fatalf("sessionIDFromRequest(nil) = %q, want empty", got)
	}
}

func TestExportedCSRFFailsClosedForNilContinuationAndContext(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, db, "sqlite")

	sessionID := "11111111-1111-4111-8111-111111111111"
	validReq := requestWithSession(http.MethodPost, sessionID)
	validReq.Header.Set("X-CSRF-TOKEN", generateCSRFToken(sessionID, csrfConfig.secret))
	bearerReq := requestWithSession(http.MethodPost, sessionID)
	bearerReq.Header.Set("Authorization", "Bearer api-token")

	for name, resp := range map[string]httpx.Response{
		"safe_method": CSRF(httpx.NewContext(requestWithSession(http.MethodGet, sessionID)), nil),
		"bearer":      CSRF(httpx.NewContext(bearerReq), nil),
		"valid_token": CSRF(httpx.NewContext(validReq), nil),
	} {
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", name, resp.StatusCode)
		}
		body, ok := resp.Body.(map[string]string)
		if !ok || body["error"] != "internal server error" {
			t.Fatalf("%s body = %#v, want sanitized internal server error", name, resp.Body)
		}
		if strings.Contains(body["error"], "panic") || strings.Contains(body["error"], "nil pointer") || strings.Contains(body["error"], sessionID) {
			t.Fatalf("%s leaked internal detail: %#v", name, body)
		}
	}

	nilCtxResp := CSRF(nil, func() httpx.Response {
		t.Fatal("CSRF should not call next for nil context")
		return httpx.Response{}
	})
	if nilCtxResp.StatusCode != http.StatusForbidden {
		t.Fatalf("nil context CSRF status = %d, want 403", nilCtxResp.StatusCode)
	}
	body, ok := nilCtxResp.Body.(map[string]string)
	if !ok || body["error"] != "CSRF request missing" {
		t.Fatalf("nil context CSRF body = %#v", nilCtxResp.Body)
	}
}

func TestExportedCSRFNonceFailuresAreSanitized(t *testing.T) {
	previous := csrfNonceReader
	csrfNonceReader = strings.NewReader("")
	defer func() { csrfNonceReader = previous }()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected CSRF nonce panic")
		}
		message, ok := recovered.(string)
		if !ok {
			t.Fatalf("panic type = %T, want string", recovered)
		}
		if message != "csrf: failed to generate random nonce" {
			t.Fatalf("panic message = %q, want sanitized nonce failure", message)
		}
		if strings.Contains(message, "EOF") || strings.Contains(message, "reader") {
			t.Fatalf("nonce panic leaked reader detail: %q", message)
		}
	}()
	_ = generateCSRFToken("11111111-1111-4111-8111-111111111111", []byte("session-secret"))
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

	validSessionID := "55555555-5555-4555-8555-555555555555"
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, payload, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", validSessionID, "user-1", "member", "{}", time.Now().Add(time.Hour), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	validCtx := httpx.NewContext(requestWithSession(http.MethodPost, validSessionID))
	if err := Put(validCtx, "secret", make(chan int)); err == nil || err.Error() != "session: invalid session value" || strings.Contains(err.Error(), "chan") || strings.Contains(err.Error(), "json:") {
		t.Fatalf("Put unsupported value error = %v, want sanitized invalid session value", err)
	}
	if err := Put(validCtx, strings.Repeat("k", 129), "value"); err == nil || err.Error() != "session: invalid session key" || strings.Contains(err.Error(), strings.Repeat("k", 64)) {
		t.Fatalf("Put oversized key error = %v, want sanitized invalid key", err)
	}
	if _, err := Get(validCtx, strings.Repeat("k", 129)); err == nil || err.Error() != "session: invalid session key" || strings.Contains(err.Error(), strings.Repeat("k", 64)) {
		t.Fatalf("Get oversized key error = %v, want sanitized invalid key", err)
	}
	if err := Put(validCtx, "blob", strings.Repeat("x", 9000)); err == nil || err.Error() != "session: payload too large" || strings.Contains(err.Error(), strings.Repeat("x", 64)) {
		t.Fatalf("Put oversized value error = %v, want sanitized payload too large", err)
	}
	var payload string
	if err := db.QueryRow("SELECT payload FROM sessions WHERE id = ?", validSessionID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != "{}" {
		t.Fatalf("rejected session writes mutated payload to %q, want {}", payload)
	}

	fullSessionID := "66666666-6666-4666-8666-666666666666"
	fullPayload := sessionPayloadWithKeys(64)
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, payload, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", fullSessionID, "user-1", "member", fullPayload, time.Now().Add(time.Hour), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	fullCtx := httpx.NewContext(requestWithSession(http.MethodPost, fullSessionID))
	if err := Put(fullCtx, "overflow", "value"); err == nil || err.Error() != "session: payload too large" || strings.Contains(err.Error(), "overflow") {
		t.Fatalf("Put full payload error = %v, want sanitized payload too large", err)
	}
	if err := db.QueryRow("SELECT payload FROM sessions WHERE id = ?", fullSessionID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != fullPayload {
		t.Fatalf("full payload changed after rejected Put")
	}

	oversizedPayloadSessionID := "77777777-7777-4777-8777-777777777777"
	if _, err := db.Exec("INSERT INTO sessions (id, user_id, role, payload, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", oversizedPayloadSessionID, "user-1", "member", ` + "`" + `{"secret":"` + "`" + `+strings.Repeat("x", 9000)+` + "`" + `"}` + "`" + `, time.Now().Add(time.Hour), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	oversizedPayloadCtx := httpx.NewContext(requestWithSession(http.MethodGet, oversizedPayloadSessionID))
	if _, err := Get(oversizedPayloadCtx, "secret"); err == nil || err.Error() != "session: invalid session payload" || strings.Contains(err.Error(), strings.Repeat("x", 64)) {
		t.Fatalf("Get oversized payload error = %v, want sanitized invalid payload", err)
	}
	if err := Put(oversizedPayloadCtx, "secret", "updated"); err == nil || err.Error() != "session: invalid session payload" || strings.Contains(err.Error(), strings.Repeat("x", 64)) {
		t.Fatalf("Put oversized payload error = %v, want sanitized invalid payload", err)
	}
}

func sessionPayloadWithKeys(keys int) string {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < keys; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(` + "`" + `"k` + "`" + `)
		if i < 10 {
			b.WriteString("0")
		}
		b.WriteString(string(rune('0' + i/10)))
		b.WriteString(string(rune('0' + i%10)))
		b.WriteString(` + "`" + `":"v"` + "`" + `)
	}
	b.WriteString("}")
	return b.String()
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

func TestExportedSessionHelpersRejectMissingDatabaseWithoutPanic(t *testing.T) {
	NewDriver(func(key, fallback string) string {
		if key == "SESSION_SECRET" {
			return "session-secret"
		}
		return fallback
	}, nil, "sqlite")
	ctx := httpx.NewContext(requestWithSession(http.MethodPost, "11111111-1111-4111-8111-111111111111"))

	check := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s should fail without database", name)
		}
		if err.Error() != "session: database not configured" {
			t.Fatalf("%s error = %v, want sanitized database-not-configured", name, err)
		}
		for _, forbidden := range []string{"panic", "nil pointer", "sessions", "SELECT", "INSERT", "DELETE"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("%s error leaked %q: %v", name, forbidden, err)
			}
		}
	}

	_, err := Create(ctx, "user-1", "member")
	check("Create", err)
	_, err = Destroy(ctx)
	check("Destroy", err)
	_, err = Get(ctx, "secret")
	check("Get", err)
	err = Put(ctx, "secret", "value")
	check("Put", err)
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
	"database/sql"
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
	req.Header.Set("X-Forwarded-For", "198.51.100.200")
	req.RemoteAddr = "192.0.2.10:1234"
	ctx := httpx.NewContext(req)
	auditActorID := uuid.New().String()
	ctx.SetAuth(&httpx.AuthInfo{UserID: auditActorID, Role: "admin"})

	var performed, denied, failed int
	var deniedReason, failedError string
	models.OnAuditPerformed = func(ctx *httpx.Context, action, model string, resourceID any, extra string) {
		if (action == "Ban" || action == "Promote") && model == "User" {
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
	authorizedRoleID, err := models.AuthorizeBan(ctx, user)
	if err != nil {
		t.Fatalf("authorize ban: %v", err)
	}
	if authorizedRoleID == nil || *authorizedRoleID == uuid.Nil {
		t.Fatalf("authorize ban role id = %v, want non-nil audit role id", authorizedRoleID)
	}
	if viewRoleID, err := models.AuthorizeView(ctx, user); err != nil || viewRoleID == nil || *viewRoleID == uuid.Nil {
		t.Fatalf("authorize standalone view = %v, %v; want non-nil role id", viewRoleID, err)
	}

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
	var auditIP string
	if err := db.Raw("SELECT ip_address FROM user_actions LIMIT 1").Scan(&auditIP).Error; err != nil {
		t.Fatal(err)
	}
	if auditIP != "192.0.2.10" {
		t.Fatalf("audit ip = %q, want unspoofed remote address", auditIP)
	}
	var auditUserID, auditResourceID, auditRequestID string
	var auditRoleID sql.NullString
	if err := db.Raw("SELECT user_id, resource_id, role_id, request_id FROM user_actions LIMIT 1").Row().Scan(&auditUserID, &auditResourceID, &auditRoleID, &auditRequestID); err != nil {
		t.Fatal(err)
	}
	if auditUserID != auditActorID {
		t.Fatalf("audit user_id = %q, want authenticated actor %q", auditUserID, auditActorID)
	}
	if auditResourceID != userID.String() {
		t.Fatalf("audit resource_id = %q, want %q", auditResourceID, userID.String())
	}
	if !auditRoleID.Valid || auditRoleID.String == "" {
		t.Fatalf("audit role_id = %#v, want gate role id", auditRoleID)
	}
	if auditRequestID != "req-123" {
		t.Fatalf("audit request_id = %q, want req-123", auditRequestID)
	}

	result, err := user.Promote(ctx, models.PromoteAction{Level: "gold"})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if result == nil || result.Level != "gold" {
		t.Fatalf("promote result = %+v, want level gold", result)
	}
	if performed != 2 {
		t.Fatalf("performed audit hook count after promote = %d, want 2", performed)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 2 {
		t.Fatalf("audit rows after result-bearing action = %d, want 2", auditRows)
	}
	var promoteRows int64
	if err := db.Raw("SELECT COUNT(*) FROM user_actions ua JOIN action_types at ON at.id = ua.action_type_id WHERE at.name = ?", "Promote").Scan(&promoteRows).Error; err != nil {
		t.Fatal(err)
	}
	if promoteRows != 1 {
		t.Fatalf("promote audit rows = %d, want 1", promoteRows)
	}

	loadedAdminCtx := httpx.NewContext(req)
	loadedAdminCtx.SetAuth(&httpx.AuthInfo{UserID: uuid.New().String(), Role: "viewer"})
	loadedAdminCtx.SetRoles([]httpx.RoleInfo{{Slug: "admin"}})
	if err := user.Ban(loadedAdminCtx, models.BanAction{Reason: "loaded-admin"}); err != nil {
		t.Fatalf("ban with loaded admin role: %v", err)
	}
	if performed != 3 {
		t.Fatalf("performed audit hook count after loaded admin ban = %d, want 3", performed)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 3 {
		t.Fatalf("audit rows after loaded admin action = %d, want 3", auditRows)
	}

	loadedEmptyCtx := httpx.NewContext(req)
	loadedEmptyCtx.SetAuth(&httpx.AuthInfo{UserID: uuid.New().String(), Role: "admin"})
	loadedEmptyCtx.SetRoles(nil)
	if err := user.Ban(loadedEmptyCtx, models.BanAction{Reason: "loaded-empty"}); !errors.Is(err, models.ErrUnauthorized) {
		t.Fatalf("ban with empty loaded roles error = %v, want ErrUnauthorized", err)
	}
	if denied != 1 || deniedReason != "gate denied" {
		t.Fatalf("denied audit hook count/reason after empty loaded roles = %d/%q, want 1/gate denied", denied, deniedReason)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 3 {
		t.Fatalf("audit rows after empty loaded roles = %d, want 3", auditRows)
	}

	badAuditCtx := httpx.NewContext(req)
	badAuditCtx.SetAuth(&httpx.AuthInfo{UserID: "not-a-uuid", Role: "admin"})
	if err := user.Ban(badAuditCtx, models.BanAction{Reason: "audit-fail"}); err == nil || err.Error() != "audit user id" {
		t.Fatalf("ban with invalid audit user id error = %v, want sanitized audit user id error", err)
	} else if strings.Contains(err.Error(), "not-a-uuid") || strings.Contains(err.Error(), "invalid UUID") {
		t.Fatalf("invalid audit user id error leaked detail: %v", err)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 3 {
		t.Fatalf("audit rows after failed audit persistence = %d, want 3", auditRows)
	}
	var persistedName string
	if err := db.Raw("SELECT name FROM users WHERE id = ?", userID.String()).Scan(&persistedName).Error; err != nil {
		t.Fatal(err)
	}
	if persistedName != "loaded-admin" {
		t.Fatalf("user name after failed audit persistence = %q, want loaded-admin", persistedName)
	}

	closedDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(closedDB)
	sqlDB, err := closedDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	err = user.Ban(ctx, models.BanAction{Reason: "closed-db"})
	if err == nil {
		t.Fatal("expected closed audit database to fail")
	}
	if err.Error() != "audit database error" {
		t.Fatalf("closed audit database error = %v, want sanitized audit database error", err)
	}
	for _, forbidden := range []string{"sql:", "closed", "user_actions", "model_types", "INSERT", "password"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("closed audit database error leaked %q: %v", forbidden, err)
		}
	}
	models.SetDB(db)

	deniedCtx := httpx.NewContext(req)
	deniedCtx.SetAuth(&httpx.AuthInfo{UserID: uuid.New().String(), Role: "viewer"})
	if roleID, err := models.AuthorizeBan(deniedCtx, user); !errors.Is(err, models.ErrUnauthorized) || roleID != nil {
		t.Fatalf("denied authorize ban = %v, %v; want nil ErrUnauthorized", roleID, err)
	}
	if err := user.Ban(deniedCtx, models.BanAction{Reason: "denied"}); !errors.Is(err, models.ErrUnauthorized) {
		t.Fatalf("denied ban error = %v, want ErrUnauthorized", err)
	}
	if denied != 2 || deniedReason != "gate denied" {
		t.Fatalf("denied audit hook count/reason = %d/%q, want 2/gate denied", denied, deniedReason)
	}
	if err := db.Table("user_actions").Count(&auditRows).Error; err != nil {
		t.Fatal(err)
	}
	if auditRows != 3 {
		t.Fatalf("audit rows after denied action = %d, want 3", auditRows)
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
	if auditRows != 3 {
		t.Fatalf("audit rows after failed action = %d, want 3", auditRows)
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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"basic-crud/internal/httpx"
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

func TestRunAuditedActionSanitizesMissingAuditSeed(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(sqliteDB)
	err = runAuditedAction(nil, "SecretModel", "PasswordReset", uuid.New(), nil, nil, func() error {
		t.Fatal("action body should not run without an audit seed")
		return nil
	})
	if !errors.Is(err, errAuditSeed) || err.Error() != "audit seed error" {
		t.Fatalf("missing audit seed error = %v, want sanitized audit seed error", err)
	}
	if strings.Contains(err.Error(), "SecretModel") || strings.Contains(err.Error(), "PasswordReset") {
		t.Fatalf("missing audit seed error leaked model/action detail: %v", err)
	}
}

func TestRunAuditedActionFailsClosedWithoutAuth(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	SetDB(sqliteDB)
	bodyRan := false
	err = runAuditedAction(nil, "User", "Ban", uuid.New(), nil, nil, func() error {
		bodyRan = true
		return nil
	})
	if !bodyRan {
		t.Fatal("action body should run before audit persistence rejects missing auth")
	}
	if !errors.Is(err, errAuditUserID) || err.Error() != "audit user id" {
		t.Fatalf("missing audit auth error = %v, want sanitized audit user id error", err)
	}
	for _, forbidden := range []string{"invalid UUID", "User", "Ban", "user_actions", "INSERT"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("missing audit auth error leaked %q: %v", forbidden, err)
		}
	}
	if sqliteDB.Migrator().HasTable("user_actions") {
		t.Fatal("audit tables should roll back when audit user id is missing")
	}
}

func TestAuditMetadataHelpersAreBounded(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/audit", nil)
	req.RemoteAddr = "192.0.2.44:1234"
	req.Header.Set("X-Request-ID", " req-123 ")
	ctx := httpx.NewContext(req)

	if got := auditContextIP(ctx); got != "192.0.2.44" {
		t.Fatalf("auditContextIP = %q, want remote IP", got)
	}
	if got := auditContextRequestID(ctx); got != "req-123" {
		t.Fatalf("auditContextRequestID = %q, want trimmed request ID", got)
	}

	req.RemoteAddr = strings.Repeat("1", maxAuditIPBytes+1)
	req.Header.Set("X-Request-ID", strings.Repeat("x", maxAuditRequestIDBytes+1))
	if got := auditContextIP(ctx); got != "" {
		t.Fatalf("oversized audit IP = %q, want empty", got)
	}
	if got := auditContextRequestID(ctx); got != "" {
		t.Fatalf("oversized audit request ID = %q, want empty", got)
	}

	req.RemoteAddr = "192.0.2.44:1234"
	req.Header.Set("X-Request-ID", "req\nsecret")
	if got := auditContextRequestID(ctx); got != "" {
		t.Fatalf("control-character audit request ID = %q, want empty", got)
	}
	req.Header.Del("X-Request-ID")
	req.Header.Set("X-Request-Id", "legacy-req")
	if got := auditContextRequestID(ctx); got != "legacy-req" {
		t.Fatalf("legacy audit request ID = %q, want legacy-req", got)
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
	"strings"
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

func TestExportedMigrationFailureDoesNotLeakStatementText(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := migrations.NewRunner(db, "sqlite")
	failingMigrate := migrations.MigrationEntry{
		ID:       "99990101000002_secret_failure",
		UpFile:   "99990101000002_secret_failure.up.sql",
		DownFile: "99990101000002_secret_failure.down.sql",
	}
	err = runner.Migrate([]migrations.MigrationEntry{failingMigrate})
	if err == nil {
		t.Fatal("expected failing migration")
	}
	if strings.Contains(err.Error(), "password=swordfish") || strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("migration error leaked statement text: %v", err)
	}
	if !strings.Contains(err.Error(), "executing migration statement 1") {
		t.Fatalf("migration error missing statement index: %v", err)
	}
}

func TestExportedMigrationFreshFailsClosedOnDownErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := migrations.NewRunner(db, "sqlite")
	failingFresh := migrations.MigrationEntry{
		ID:       "99990101000003_fresh_down_failure",
		UpFile:   "99990101000003_fresh_down_failure.up.sql",
		DownFile: "99990101000003_fresh_down_failure.down.sql",
	}
	if err := runner.Migrate([]migrations.MigrationEntry{failingFresh}); err != nil {
		t.Fatalf("setup migration: %v", err)
	}
	assertSQLiteTableExists(t, db, "fresh_down_failure")
	assertMigrationRowCount(t, db, failingFresh.ID, 1)

	err = runner.Fresh([]migrations.MigrationEntry{failingFresh})
	if err == nil {
		t.Fatal("expected fresh to fail on down migration error")
	}
	if strings.Contains(err.Error(), "password=swordfish") || strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("fresh error leaked statement text: %v", err)
	}
	if !strings.Contains(err.Error(), "fresh rollback "+failingFresh.ID) || !strings.Contains(err.Error(), "executing migration statement 1") {
		t.Fatalf("fresh error missing migration and statement context: %v", err)
	}
	assertSQLiteTableExists(t, db, "fresh_down_failure")
	assertMigrationRowCount(t, db, failingFresh.ID, 1)
}

func TestExportedMigrationRegistryRejectsUnsafeEntries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := migrations.NewRunner(db, "sqlite")
	unsafe := migrations.MigrationEntry{
		ID:       "99990101000004_unsafe_file",
		UpFile:   "../password=swordfish.sql",
		DownFile: "99990101000004_unsafe_file.down.sql",
	}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "migrate", run: func() error { return runner.Migrate([]migrations.MigrationEntry{unsafe}) }},
		{name: "rollback", run: func() error { return runner.Rollback([]migrations.MigrationEntry{unsafe}) }},
		{name: "fresh", run: func() error { return runner.Fresh([]migrations.MigrationEntry{unsafe}) }},
		{name: "status", run: func() error { _, err := runner.Status([]migrations.MigrationEntry{unsafe}); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || err.Error() != "migration file name invalid" {
				t.Fatalf("%s error = %v, want sanitized invalid filename", tc.name, err)
			}
			for _, forbidden := range []string{"swordfish", "password", "..", "../", "SELECT"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("%s leaked %q in error: %v", tc.name, forbidden, err)
				}
			}
		})
	}
	if rows := countMigrationRows(t, db); rows != 0 {
		t.Fatalf("unsafe migration entry recorded %d rows, want 0", rows)
	}
}

func TestExportedMigrationRunnerRejectsNilDBWithoutPanic(t *testing.T) {
	runner := migrations.NewRunner(nil, "sqlite")
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "migrate", run: func() error { return runner.Migrate(nil) }},
		{name: "rollback", run: func() error { return runner.Rollback(nil) }},
		{name: "fresh", run: func() error { return runner.Fresh(nil) }},
		{name: "status", run: func() error { _, err := runner.Status(nil); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil || err.Error() != "migrations: DB is nil" {
				t.Fatalf("%s error = %v, want migrations: DB is nil", tc.name, err)
			}
		})
	}
}

func TestExportedMigrationDatabaseErrorsAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*migrations.Runner) error
	}{
		{name: "migrate", run: func(r *migrations.Runner) error { return r.Migrate(nil) }},
		{name: "rollback", run: func(r *migrations.Runner) error { return r.Rollback(nil) }},
		{name: "fresh", run: func(r *migrations.Runner) error { return r.Fresh(nil) }},
		{name: "status", run: func(r *migrations.Runner) error { _, err := r.Status(nil); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}
			err = tc.run(migrations.NewRunner(db, "sqlite"))
			if err == nil {
				t.Fatalf("%s should fail on closed database", tc.name)
			}
			if err.Error() != "migration database error" {
				t.Fatalf("%s error = %v, want sanitized migration database error", tc.name, err)
			}
			for _, forbidden := range []string{"sql:", "closed", "SELECT", "CREATE TABLE", "migrations"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("%s migration database error leaked %q: %v", tc.name, forbidden, err)
				}
			}
		})
	}
}

func countMigrationRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	if !db.Migrator().HasTable("migrations") {
		return 0
	}
	var count int64
	if err := db.Table("migrations").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
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
		"99990101000000_atomic_failure.up.sql":       "CREATE TABLE atomic_failure (id TEXT PRIMARY KEY);\nSELECT * FROM definitely_missing_table;\n",
		"99990101000000_atomic_failure.down.sql":     "DROP TABLE IF EXISTS atomic_failure;\n",
		"99990101000001_atomic_rollback.up.sql":      "CREATE TABLE atomic_rollback (id TEXT PRIMARY KEY);\n",
		"99990101000001_atomic_rollback.down.sql":    "DROP TABLE atomic_rollback;\nSELECT * FROM definitely_missing_table;\n",
		"99990101000002_secret_failure.up.sql":       "SELECT 'password=swordfish' FROM definitely_missing_table;\n",
		"99990101000002_secret_failure.down.sql":     "SELECT 1;\n",
		"99990101000003_fresh_down_failure.up.sql":   "CREATE TABLE fresh_down_failure (id TEXT PRIMARY KEY);\n",
		"99990101000003_fresh_down_failure.down.sql": "SELECT 'password=swordfish' FROM definitely_missing_table;\nDROP TABLE fresh_down_failure;\n",
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
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("%s %s Content-Type = %q, want application/json", tc.method, tc.path, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("%s %s X-Content-Type-Options = %q, want nosniff", tc.method, tc.path, got)
		}
		if strings.Contains(rec.Body.String(), "pickle") || !strings.Contains(rec.Body.String(), "not found") {
			t.Fatalf("%s %s not found body = %s", tc.method, tc.path, rec.Body.String())
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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportedCommandFatalMessagesAreSanitized(t *testing.T) {
	for _, msg := range []string{
		commandFailureMessage(),
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
	if strings.Contains(commandFailureMessage(), "migrate") || strings.Contains(commandFailureMessage(), "token=secret") {
		t.Fatalf("command failure message should not include command names: %s", commandFailureMessage())
	}
}

func TestExportedCommandAppHandlesNilPieces(t *testing.T) {
	var nilApp *App
	nilApp.Run(nil)
	nilApp.PrintCommands()

	var nilCommand Command
	app := BuildApp(nil, nil, nilCommand, emptyNameCommand{}, secretNameCommand{})
	app.Run(nil)
	app.PrintCommands()
	if _, ok := app.commands[""]; ok {
		t.Fatal("empty command name should not be registered")
	}
	if _, ok := app.commands["deploy-password=swordfish"]; !ok {
		t.Fatal("valid custom command should be registered")
	}
	if strings.Contains(commandFailureMessage(), "deploy-password=swordfish") {
		t.Fatalf("command failure message leaked custom command name: %s", commandFailureMessage())
	}

	first := &countingCommand{name: "duplicate"}
	second := &countingCommand{name: "duplicate"}
	duplicateApp := BuildApp(nil, nil, first, second)
	if duplicateApp.commands["duplicate"] != first {
		t.Fatal("duplicate command registration should preserve first command")
	}
}

func TestExportedCommandDispatchChecksUnknownCommandsBeforeStartup(t *testing.T) {
	initialized := 0
	app := BuildApp(func() {
		initialized++
	}, nil, &countingCommand{name: "known"})

	err := app.run([]string{"missing-password=swordfish"})
	if err == nil || err.Error() != "unknown command" {
		t.Fatalf("unknown command error = %v, want sanitized unknown command", err)
	}
	if initialized != 0 {
		t.Fatalf("unknown command initialized startup %d times, want 0", initialized)
	}
	if strings.Contains(err.Error(), "swordfish") || strings.Contains(err.Error(), "missing-password") {
		t.Fatalf("unknown command error leaked command detail: %v", err)
	}
}

func TestExportedCommandDispatchInitializesKnownCommandsAndServer(t *testing.T) {
	initialized := 0
	served := 0
	command := &countingCommand{name: "known"}
	app := BuildApp(func() {
		initialized++
	}, func() {
		served++
	}, command)

	if err := app.run([]string{"known", "one", "two"}); err != nil {
		t.Fatalf("known command error: %v", err)
	}
	if initialized != 1 || command.runs != 1 || served != 0 {
		t.Fatalf("known command initialized/runs/served = %d/%d/%d, want 1/1/0", initialized, command.runs, served)
	}
	if got := strings.Join(command.args, ","); got != "one,two" {
		t.Fatalf("known command args = %q, want one,two", got)
	}

	if err := app.run(nil); err != nil {
		t.Fatalf("server mode error: %v", err)
	}
	if initialized != 2 || command.runs != 1 || served != 1 {
		t.Fatalf("server mode initialized/runs/served = %d/%d/%d, want 2/1/1", initialized, command.runs, served)
	}
}

func TestExportedCommandAppRegistersAndRunsUserCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "custom-command.sqlite")
	markerPath := filepath.Join(t.TempDir(), "audit-marker.txt")
	t.Setenv("DB_CONNECTION", "sqlite")
	t.Setenv("DB_DATABASE", dbPath)
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("APP_ENCRYPTION_KEY", "12345678901234567890123456789012")
	t.Setenv("AUDIT_COMMAND_MARKER", markerPath)

	app := NewApp()
	if _, ok := app.commands["audit:marker"]; !ok {
		t.Fatalf("exported app did not register user command; commands = %#v", app.commands)
	}
	if err := app.run([]string{"audit:marker"}); err != nil {
		t.Fatalf("user command failed: %v", err)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if string(data) != "ran" {
		t.Fatalf("marker = %q, want ran", data)
	}
}

func TestExportedCommandAppDoesNotLetUserCommandsOverrideBuiltins(t *testing.T) {
	app := NewApp()
	cmd, ok := app.commands["migrate"]
	if !ok {
		t.Fatal("migrate command was not registered")
	}
	if _, ok := cmd.(migrateCommand); !ok {
		t.Fatalf("migrate command type = %T, want built-in migrateCommand", cmd)
	}
}

type assertSecretError string

func (e assertSecretError) Error() string { return string(e) }

type emptyNameCommand struct{}

func (emptyNameCommand) Name() string { return "" }
func (emptyNameCommand) Description() string { return "empty" }
func (emptyNameCommand) Run(args []string) error { return nil }

type secretNameCommand struct{}

func (secretNameCommand) Name() string { return "deploy-password=swordfish" }
func (secretNameCommand) Description() string { return "secret name" }
func (secretNameCommand) Run(args []string) error { return assertSecretError("password=swordfish") }

type countingCommand struct {
	name string
	runs int
	args []string
}

func (c *countingCommand) Name() string { return c.name }
func (c *countingCommand) Description() string { return "counting" }
func (c *countingCommand) Run(args []string) error {
	c.runs++
	c.args = append([]string{}, args...)
	return nil
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "commands", "exported_command_security_test.go"), []byte(securityTestSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	cliTestSrc := `package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestExportedServerBinaryRunsMigrationCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "binary.sqlite")
	cmd := exec.Command("go", "run", ".", "migrate")
	cmd.Env = append(os.Environ(),
		"DB_CONNECTION=sqlite",
		"DB_DATABASE="+dbPath,
		"JWT_SECRET=0123456789abcdef0123456789abcdef",
		"APP_ENCRYPTION_KEY=12345678901234567890123456789012",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . migrate failed: %v\n%s", err, output)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := countBinaryRows(t, db, "migrations"); got == 0 {
		t.Fatal("binary migrate did not record app migrations")
	}
	if got := countBinaryRows(t, db, "roles"); got != 3 {
		t.Fatalf("binary migrate roles = %d, want 3", got)
	}
	if got := countBinaryWhere(t, db, "graphql_exposures", "model = 'users' AND operation = 'list'"); got != 1 {
		t.Fatalf("binary migrate graphql exposure count = %d, want 1", got)
	}
}

func TestExportedServerBinaryRejectsUnknownCommandBeforeStartup(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "missing-password=swordfish")
	cmd.Env = append(os.Environ(), "DB_CONNECTION=sqlite", "DB_DATABASE=/definitely/missing/exported.sqlite")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("unknown command unexpectedly succeeded: %s", output)
	}
	text := string(output)
	if !strings.Contains(text, "unknown command") {
		t.Fatalf("unknown command output = %s, want sanitized marker", text)
	}
	for _, leak := range []string{"swordfish", "missing-password", "database startup failed", "no such file", "/definitely/missing", "Available commands", "audit:marker", "Shadow built-in", "migrate:rollback"} {
		if strings.Contains(text, leak) {
			t.Fatalf("unknown command output leaked %q: %s", leak, text)
		}
	}
}

func TestExportedServerBinaryServesHTTP(t *testing.T) {
	port := freeBinaryPort(t)
	dbPath := filepath.Join(t.TempDir(), "server.sqlite")
	server := startExportedBinaryServer(t, port, dbPath)
	defer server.cleanup(t)

	url := "http://127.0.0.1:" + port + "/pickle/health"
	var resp *http.Response
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-server.done:
			server.doneConsumed = true
			t.Fatalf("exported server binary exited before serving: %v\n%s", waitErr, server.output.String())
		default:
		}
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("exported server binary did not serve %s: %v\n%s", url, err, server.output.String())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("server status = %d body=%s output=%s", resp.StatusCode, body, server.output.String())
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("server Content-Type = %q, want application/json", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("server X-Content-Type-Options = %q, want nosniff", got)
	}
	text := string(body)
	if !strings.Contains(text, "not found") {
		t.Fatalf("server body = %s, want not found", text)
	}
	if strings.Contains(text, "pickle") || strings.Contains(text, "config") {
		t.Fatalf("server body leaked legacy route detail: %s", text)
	}
}

func TestExportedServerBinaryServesMigratedRoutes(t *testing.T) {
	port := freeBinaryPort(t)
	dbPath := filepath.Join(t.TempDir(), "server-routes.sqlite")
	migrate := exec.Command("go", "run", ".", "migrate")
	migrate.Env = exportedBinaryEnv(port, dbPath)
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("go run . migrate failed: %v\n%s", err, output)
	}

	server := startExportedBinaryServer(t, port, dbPath)
	defer server.cleanup(t)

	body := ` + "`" + `{"name":"Ada","email":"ada@example.com","password":"correct horse"}` + "`" + `
	url := "http://127.0.0.1:" + port + "/api/users"
	var resp *http.Response
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-server.done:
			server.doneConsumed = true
			t.Fatalf("exported server binary exited before route request: %v\n%s", waitErr, server.output.String())
		default:
		}
		resp, err = http.Post(url, "application/json", strings.NewReader(body))
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("exported server binary did not serve %s: %v\n%s", url, err, server.output.String())
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create user status = %d body=%s output=%s", resp.StatusCode, respBody, server.output.String())
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("create user Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(string(respBody), "Ada") {
		t.Fatalf("create user response = %s, want Ada", respBody)
	}

	graphQLBody := ` + "`" + `{"query":"{ __schema { queryType { name } } }"}` + "`" + `
	graphQLResp, err := http.Post("http://127.0.0.1:"+port+"/graphql", "application/json", strings.NewReader(graphQLBody))
	if err != nil {
		t.Fatalf("exported server binary did not serve /graphql: %v\n%s", err, server.output.String())
	}
	defer graphQLResp.Body.Close()
	graphQLRespBody, err := io.ReadAll(graphQLResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if graphQLResp.StatusCode != http.StatusOK {
		t.Fatalf("graphql status = %d body=%s output=%s", graphQLResp.StatusCode, graphQLRespBody, server.output.String())
	}
	if got := graphQLResp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("graphql Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(string(graphQLRespBody), "GraphQL introspection is disabled") {
		t.Fatalf("graphql route did not use hardened gqlgen handler: %s", graphQLRespBody)
	}

	usersBody := ` + "`" + `{"query":"{ users { totalCount edges { node { id name email } } } }"}` + "`" + `
	usersResp, err := http.Post("http://127.0.0.1:"+port+"/graphql", "application/json", strings.NewReader(usersBody))
	if err != nil {
		t.Fatalf("exported server binary did not serve GraphQL users query: %v\n%s", err, server.output.String())
	}
	defer usersResp.Body.Close()
	usersRespBody, err := io.ReadAll(usersResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if usersResp.StatusCode != http.StatusOK {
		t.Fatalf("graphql users status = %d body=%s output=%s", usersResp.StatusCode, usersRespBody, server.output.String())
	}
	if !strings.Contains(string(usersRespBody), "UNAUTHENTICATED") {
		t.Fatalf("unauthenticated graphql users query should fail closed: %s", usersRespBody)
	}

	loginBody := ` + "`" + `{"email":"ada@example.com","password":"correct horse"}` + "`" + `
	loginResp, err := http.Post("http://127.0.0.1:"+port+"/api/login", "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("exported server binary did not serve /api/login: %v\n%s", err, server.output.String())
	}
	defer loginResp.Body.Close()
	loginRespBody, err := io.ReadAll(loginResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d body=%s output=%s", loginResp.StatusCode, loginRespBody, server.output.String())
	}
	var loginPayload struct {
		Token string ` + "`" + `json:"token"` + "`" + `
	}
	if err := json.Unmarshal(loginRespBody, &loginPayload); err != nil {
		t.Fatalf("decode login response: %v body=%s", err, loginRespBody)
	}
	if loginPayload.Token == "" {
		t.Fatalf("login response missing token: %s", loginRespBody)
	}

	protectedURL := "http://127.0.0.1:" + port + "/api/users"
	protectedResp, err := http.Get(protectedURL)
	if err != nil {
		t.Fatalf("exported server binary did not serve protected users route: %v\n%s", err, server.output.String())
	}
	defer protectedResp.Body.Close()
	protectedRespBody, err := io.ReadAll(protectedResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if protectedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated protected users status = %d body=%s", protectedResp.StatusCode, protectedRespBody)
	}

	protectedReq, err := http.NewRequest(http.MethodGet, protectedURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	protectedReq.Header.Set("Authorization", "Bearer "+loginPayload.Token)
	authProtectedResp, err := http.DefaultClient.Do(protectedReq)
	if err != nil {
		t.Fatalf("authenticated protected users route failed: %v\n%s", err, server.output.String())
	}
	defer authProtectedResp.Body.Close()
	authProtectedRespBody, err := io.ReadAll(authProtectedResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if authProtectedResp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated protected users status = %d body=%s output=%s", authProtectedResp.StatusCode, authProtectedRespBody, server.output.String())
	}
	authProtectedText := string(authProtectedRespBody)
	for _, want := range []string{` + "`" + `"name":"Ada"` + "`" + `, ` + "`" + `"email":"ada@example.com"` + "`" + `} {
		if !strings.Contains(authProtectedText, want) {
			t.Fatalf("authenticated protected users response missing %s: %s", want, authProtectedText)
		}
	}
	for _, leak := range []string{"password", "passwordHash", "password_hash", "correct horse"} {
		if strings.Contains(authProtectedText, leak) {
			t.Fatalf("authenticated protected users response leaked %q: %s", leak, authProtectedText)
		}
	}

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/graphql", strings.NewReader(usersBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginPayload.Token)
	authUsersResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated GraphQL users query failed: %v\n%s", err, server.output.String())
	}
	defer authUsersResp.Body.Close()
	authUsersRespBody, err := io.ReadAll(authUsersResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if authUsersResp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated graphql users status = %d body=%s output=%s", authUsersResp.StatusCode, authUsersRespBody, server.output.String())
	}
	if strings.Contains(string(authUsersRespBody), "UNAUTHENTICATED") {
		t.Fatalf("authenticated graphql users query was denied: %s", authUsersRespBody)
	}
	usersText := string(authUsersRespBody)
	for _, want := range []string{` + "`" + `"totalCount":1` + "`" + `, ` + "`" + `"name":"Ada"` + "`" + `, ` + "`" + `"email":"ada@example.com"` + "`" + `} {
		if !strings.Contains(usersText, want) {
			t.Fatalf("graphql users response missing %s: %s", want, usersText)
		}
	}
	for _, leak := range []string{"password", "passwordHash", "password_hash", "correct horse"} {
		if strings.Contains(usersText, leak) {
			t.Fatalf("graphql users response leaked %q: %s", leak, usersText)
		}
	}
}

func countBinaryRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	return countBinaryWhere(t, db, table, "1 = 1")
}

type exportedBinaryServer struct {
	cmd          *exec.Cmd
	output       *bytes.Buffer
	done         chan error
	doneConsumed bool
}

func startExportedBinaryServer(t *testing.T, port, dbPath string) *exportedBinaryServer {
	t.Helper()
	cmd := exec.Command("go", "run", ".")
	cmd.Env = exportedBinaryEnv(port, dbPath)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start exported server binary: %v", err)
	}
	server := &exportedBinaryServer{
		cmd:    cmd,
		output: &output,
		done:   make(chan error, 1),
	}
	go func() { server.done <- cmd.Wait() }()
	return server
}

func (s *exportedBinaryServer) cleanup(t *testing.T) {
	t.Helper()
	if s == nil || s.doneConsumed {
		return
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("exported server binary did not exit after kill; output=%s", s.output.String())
	}
}

func exportedBinaryEnv(port, dbPath string) []string {
	return append(os.Environ(),
		"APP_PORT="+port,
		"DB_CONNECTION=sqlite",
		"DB_DATABASE="+dbPath,
		"JWT_SECRET=0123456789abcdef0123456789abcdef",
		"APP_ENCRYPTION_KEY=12345678901234567890123456789012",
	)
}

func freeBinaryPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func countBinaryWhere(t *testing.T, db *sql.DB, table, where string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE " + where).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
`
	if err := os.WriteFile(filepath.Join(out, "cmd", "server", "exported_cli_test.go"), []byte(cliTestSrc), 0o644); err != nil {
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
	var rbacChangelogRows int64
	if err := db.Table("rbac_changelog").Count(&rbacChangelogRows).Error; err != nil {
		t.Fatal(err)
	}
	var graphqlChangelogRows int64
	if err := db.Table("graphql_changelog").Count(&graphqlChangelogRows).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("second policy migrate: %v", err)
	}
	if err := db.Table("roles").Count(&roles).Error; err != nil {
		t.Fatal(err)
	}
	if roles != 3 {
		t.Fatalf("roles after second migrate = %d, want 3", roles)
	}
	if err := db.Table("role_actions").Where("role_slug = ? AND action = ?", "admin", "users.create").Count(&adminCreates).Error; err != nil {
		t.Fatal(err)
	}
	if adminCreates != 1 {
		t.Fatalf("admin users.create grants after second migrate = %d, want 1", adminCreates)
	}
	if err := db.Table("graphql_exposures").Where("model = ? AND operation = ?", "users", "list").Count(&userList).Error; err != nil {
		t.Fatal(err)
	}
	if userList != 1 {
		t.Fatalf("graphql users.list exposures after second migrate = %d, want 1", userList)
	}
	var rbacChangelogRowsAfter int64
	if err := db.Table("rbac_changelog").Count(&rbacChangelogRowsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if rbacChangelogRowsAfter != rbacChangelogRows {
		t.Fatalf("rbac changelog rows after second migrate = %d, want stable %d", rbacChangelogRowsAfter, rbacChangelogRows)
	}
	var graphqlChangelogRowsAfter int64
	if err := db.Table("graphql_changelog").Count(&graphqlChangelogRowsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if graphqlChangelogRowsAfter != graphqlChangelogRows {
		t.Fatalf("graphql changelog rows after second migrate = %d, want stable %d", graphqlChangelogRowsAfter, graphqlChangelogRows)
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
	for _, table := range []string{"role_actions", "role_user", "rbac_changelog", "graphql_exposures", "graphql_actions", "graphql_changelog"} {
		var rows int64
		if err := db.Table(table).Count(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("%s rows after rollback = %d, want 0", table, rows)
		}
	}
	if err := Fresh(db, "sqlite"); err != nil {
		t.Fatalf("policy fresh: %v", err)
	}
	if err := db.Table("roles").Count(&roles).Error; err != nil {
		t.Fatal(err)
	}
	if roles != 3 {
		t.Fatalf("roles after fresh = %d, want 3", roles)
	}
	if err := db.Table("graphql_exposures").Where("model = ? AND operation = ?", "users", "list").Count(&userList).Error; err != nil {
		t.Fatal(err)
	}
	if userList != 1 {
		t.Fatalf("graphql users.list exposures after fresh = %d, want 1", userList)
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
	} else if err.Error() != "policy database error" {
		t.Fatalf("policy migrate seed failure error = %v, want sanitized policy database error", err)
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

func TestExportedPolicyOperationsRejectNilDBWithoutPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "migrate", run: func() error { return Migrate(nil, "sqlite") }},
		{name: "rollback", run: func() error { return Rollback(nil, "sqlite") }},
		{name: "fresh", run: func() error { return Fresh(nil, "sqlite") }},
		{name: "status", run: func() error { _, err := Status(nil, "sqlite"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil || err.Error() != "policies: DB is nil" {
				t.Fatalf("%s error = %v, want policies: DB is nil", tc.name, err)
			}
		})
	}
}

func TestExportedPolicyStateTableNamesAreAllowlisted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policyStateTableName("rbac_changelog"); err != nil {
		t.Fatalf("rbac_changelog rejected: %v", err)
	}
	if _, err := policyAppliedUpsertSQL(db, "graphql_changelog"); err != nil {
		t.Fatalf("graphql_changelog upsert rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "state name", run: func() error { _, err := policyStateTableName("rbac_changelog; DROP TABLE roles -- password=swordfish"); return err }},
		{name: "upsert", run: func() error { _, err := policyAppliedUpsertSQL(db, "graphql_changelog; DROP TABLE graphql_actions -- password=swordfish"); return err }},
		{name: "applied rows", run: func() error { _, err := appliedPolicyRows(db, "rbac_changelog; DROP TABLE roles -- password=swordfish"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || err.Error() != "policy database error" {
				t.Fatalf("%s error = %v, want sanitized policy database error", tc.name, err)
			}
			for _, forbidden := range []string{"DROP", "password", "swordfish", "roles", "graphql_actions", ";"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("%s error leaked %q: %v", tc.name, forbidden, err)
				}
			}
		})
	}
}

func TestExportedPolicyDatabaseErrorsAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{name: "migrate", run: func(db *gorm.DB) error { return Migrate(db, "sqlite") }},
		{name: "rollback", run: func(db *gorm.DB) error { return Rollback(db, "sqlite") }},
		{name: "status", run: func(db *gorm.DB) error { _, err := Status(db, "sqlite"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}
			err = tc.run(db)
			if err == nil {
				t.Fatalf("%s should fail on closed database", tc.name)
			}
			if err.Error() != "policy database error" {
				t.Fatalf("%s error = %v, want sanitized policy database error", tc.name, err)
			}
			for _, forbidden := range []string{"sql:", "closed", "SELECT", "CREATE TABLE", "roles", "graphql"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("%s policy database error leaked %q: %v", tc.name, forbidden, err)
				}
			}
		})
	}
}

func TestExportedPolicyFreshFailsClosedOnDropErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	err = Fresh(db, "sqlite")
	if err == nil {
		t.Fatal("expected policy fresh to fail on closed database")
	}
	if !strings.Contains(err.Error(), "policy fresh drop") {
		t.Fatalf("policy fresh error = %v, want drop context", err)
	}
	if strings.Contains(err.Error(), "sql:") || strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "DROP TABLE") {
		t.Fatalf("policy fresh error leaked detail: %v", err)
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
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

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

func TestExportedResponseWriteHandlesNilInputs(t *testing.T) {
	httpx.Response{StatusCode: http.StatusAccepted, Body: map[string]string{"ok": "true"}}.
		WithCookie(nil).
		Write(nil)

	rec := httptest.NewRecorder()
	resp := httpx.Response{StatusCode: http.StatusCreated, Body: map[string]string{"ok": "true"}}.
		WithCookie(nil).
		WithCookie(&http.Cookie{Name: "sid", Value: "abc", Path: "/", HttpOnly: true})
	resp.Write(rec)
	if rec.Code != http.StatusCreated {
		t.Fatalf("response status = %d, want 201", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "panic") || strings.Contains(rec.Body.String(), "nil pointer") {
		t.Fatalf("response leaked internal detail: %s", rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "sid" {
		t.Fatalf("response cookies = %#v, want one sid cookie", cookies)
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

func TestExportedParamUUIDErrorsAreSanitized(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	router := httpx.Routes(func(r *httpx.Router) {
		r.Get("/items/:id", func(ctx *httpx.Context) httpx.Response {
			id, err := ctx.ParamUUID("id")
			if err != nil {
				return ctx.BadRequest(err.Error())
			}
			return ctx.JSON(http.StatusOK, map[string]string{"id": id.String()})
		})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/password=swordfish", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password=swordfish") || strings.Contains(rec.Body.String(), "swordfish") {
		t.Fatalf("ParamUUID response leaked route value: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid uuid parameter") {
		t.Fatalf("ParamUUID response missing sanitized error: %s", rec.Body.String())
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

func TestExportedRouterNilInputsFailClosed(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")

	router := httpx.Routes(nil)
	if router == nil {
		t.Fatal("Routes(nil) returned nil router")
	}
	if routes := router.AllRoutes(); len(routes) != 0 {
		t.Fatalf("Routes(nil).AllRoutes() = %#v, want empty", routes)
	}

	var nilRouter *httpx.Router
	nilRouter.OnError(func(ctx *httpx.Context, err error) {
		t.Fatal("nil router OnError should not register a callback")
	})
	nilRouter.Group("/api", func(r *httpx.Router) {
		t.Fatal("nil router Group should not invoke body")
	})
	nilRouter.Get("/health", func(ctx *httpx.Context) httpx.Response {
		t.Fatal("nil router Get should not register handler")
		return httpx.Response{}
	})
	nilRouter.RegisterRoutes(nil)
	nilRouter.RegisterRoutes(http.NewServeMux())
	if routes := nilRouter.AllRoutes(); routes != nil {
		t.Fatalf("nil router AllRoutes = %#v, want nil", routes)
	}

	nilReqRec := httptest.NewRecorder()
	router.ServeHTTP(nilReqRec, nil)
	if nilReqRec.Code != http.StatusBadRequest {
		t.Fatalf("nil request status = %d body=%s", nilReqRec.Code, nilReqRec.Body.String())
	}
	if strings.Contains(nilReqRec.Body.String(), "panic") || strings.Contains(nilReqRec.Body.String(), "nil pointer") {
		t.Fatalf("nil request response leaked internals: %s", nilReqRec.Body.String())
	}

	nilRouterRec := httptest.NewRecorder()
	nilRouter.ServeHTTP(nilRouterRec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if nilRouterRec.Code != http.StatusNotFound {
		t.Fatalf("nil router ServeHTTP status = %d body=%s", nilRouterRec.Code, nilRouterRec.Body.String())
	}
	if strings.Contains(nilRouterRec.Body.String(), "panic") || strings.Contains(nilRouterRec.Body.String(), "nil pointer") {
		t.Fatalf("nil router response leaked internals: %s", nilRouterRec.Body.String())
	}
}

func TestExportedRouterDirectMissUsesHardenedJSONNotFound(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	router := httpx.Routes(func(r *httpx.Router) {
		r.Get("/known", func(ctx *httpx.Context) httpx.Response {
			return ctx.NoContent()
		})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pickle/config/reload", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("direct router miss status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("direct router miss Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("direct router miss X-Content-Type-Options = %q, want nosniff", got)
	}
	if !strings.Contains(rec.Body.String(), "\"not found\"") {
		t.Fatalf("direct router miss body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "pickle") || strings.Contains(rec.Body.String(), "config") || strings.Contains(rec.Body.String(), "404 page") {
		t.Fatalf("direct router miss leaked route/default text: %s", rec.Body.String())
	}
}

func TestExportedRouterMethodMismatchUsesHardenedJSONMethodNotAllowed(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	router := httpx.Routes(func(r *httpx.Router) {
		r.Get("/known", func(ctx *httpx.Context) httpx.Response {
			return ctx.NoContent()
		})
		r.Post("/known", func(ctx *httpx.Context) httpx.Response {
			return ctx.NoContent()
		})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/known", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method mismatch status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Allow"); got != "GET, POST" {
		t.Fatalf("Allow = %q, want GET, POST", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("method mismatch Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("method mismatch X-Content-Type-Options = %q, want nosniff", got)
	}
	if !strings.Contains(rec.Body.String(), "method not allowed") {
		t.Fatalf("method mismatch body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/known") || strings.Contains(rec.Body.String(), "DELETE") {
		t.Fatalf("method mismatch response leaked route detail: %s", rec.Body.String())
	}

	miss := httptest.NewRecorder()
	router.ServeHTTP(miss, httptest.NewRequest(http.MethodDelete, "/unknown", nil))
	if miss.Code != http.StatusNotFound || miss.Header().Get("Allow") != "" {
		t.Fatalf("unknown path status/allow = %d/%q, want 404/empty", miss.Code, miss.Header().Get("Allow"))
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
		} else if message := fmt.Sprint(recovered); strings.Contains(message, "pickle") {
			t.Fatalf("duplicate route panic leaked framework name: %q", message)
		}
	}()
	router.RegisterRoutes(http.NewServeMux())
}

func TestExportedFrameworkMisusePanicsDoNotMentionPickle(t *testing.T) {
	assertPanicDoesNotMentionPickle(t, "missing param", func() {
		httpx.NewContext(httptest.NewRequest(http.MethodGet, "/", nil)).Param("id")
	})
	assertPanicDoesNotMentionPickle(t, "invalid auth", func() {
		httpx.NewContext(httptest.NewRequest(http.MethodGet, "/", nil)).SetAuth("admin")
	})
	assertPanicDoesNotMentionPickle(t, "invalid middleware", func() {
		httpx.Routes(func(r *httpx.Router) {
			r.Get("/bad", func(ctx *httpx.Context) httpx.Response {
				return ctx.NoContent()
			}, "not middleware")
		})
	})
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
	if strings.Contains(logs.String(), "goroutine ") || strings.Contains(logs.String(), ".ServeHTTP(") {
		t.Fatalf("panic log leaked stack detail: %s", logs.String())
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

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "sql err no rows", err: sql.ErrNoRows},
		{name: "wrapped sql err no rows", err: fmt.Errorf("lookup failed: %w", sql.ErrNoRows)},
		{name: "gorm record not found", err: gorm.ErrRecordNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			missing := &resourceQuery{err: tc.err}
			resp = ctx.Resource(missing)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("missing resource status = %d", resp.StatusCode)
			}
		})
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

func TestExportedResponseWriteFailsClosedOnEncodeError(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	rec := httptest.NewRecorder()
	httpx.Response{
		StatusCode: http.StatusOK,
		Body:      secretMarshalBody{},
	}.Write(rec)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if rec.Body.String() != "{\"error\":\"internal server error\"}\n" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	for _, forbidden := range []string{"swordfish", "password", "json:", "MarshalJSON"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, rec.Body.String())
		}
		if strings.Contains(logs.String(), forbidden) {
			t.Fatalf("log leaked %q: %s", forbidden, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "http response encode failed") {
		t.Fatalf("log missing sanitized marker: %s", logs.String())
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

type secretMarshalBody struct{}

func (secretMarshalBody) MarshalJSON() ([]byte, error) {
	return nil, errors.New("database password is swordfish")
}

func assertPanicDoesNotMentionPickle(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("%s should panic", name)
		}
		message := fmt.Sprint(recovered)
		if strings.Contains(message, "pickle") {
			t.Fatalf("%s panic leaked framework name: %q", name, message)
		}
	}()
	fn()
}
`
	if err := os.WriteFile(filepath.Join(out, "internal", "httpx", "exported_router_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	proxyTestSrc := `package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestExportedContextClientIPIgnoresSpoofedProxyHeadersByDefault(t *testing.T) {
	resetTrustedProxyStateForTest()
	t.Setenv("TRUSTED_PROXIES", "")
	ctx := NewContext(requestFrom("10.0.0.1:1234", "198.51.100.77"))
	if got := ctx.ClientIP(); got != "10.0.0.1" {
		t.Fatalf("ClientIP = %q, want remote address without trusting X-Forwarded-For", got)
	}
}

func TestExportedContextHelpersHandleNilRequest(t *testing.T) {
	ctx := NewContext(nil)
	ctx.SetParam("id", "123")
	if got := ctx.Param("id"); got != "123" {
		t.Fatalf("Param after SetParam = %q, want 123", got)
	}
	ctx.params = nil
	ctx.SetParam("restored", "456")
	if got := ctx.Param("restored"); got != "456" {
		t.Fatalf("Param after SetParam on nil params = %q, want 456", got)
	}
	ctx.SetAuth(&AuthInfo{UserID: "user-1", Role: "admin"})
	if !ctx.IsAuthenticated() || ctx.Auth().UserID != "user-1" {
		t.Fatalf("SetAuth did not authenticate context: %#v", ctx.Auth())
	}
	ctx.SetAuth(nil)
	if ctx.IsAuthenticated() || ctx.Auth().UserID != "" {
		t.Fatalf("SetAuth(nil) should clear auth, got %#v", ctx.Auth())
	}
	ctx.SetRoles([]RoleInfo{{Slug: "admin", Manages: true}})
	if !ctx.HasRole("admin") || !ctx.IsAdmin() {
		t.Fatalf("SetRoles did not load admin role: roles=%#v admin=%v", ctx.Roles(), ctx.IsAdmin())
	}
	ctx.SetRoles(nil)
	if ctx.HasRole("admin") || ctx.IsAdmin() {
		t.Fatalf("SetRoles(nil) should clear role state: roles=%#v admin=%v", ctx.Roles(), ctx.IsAdmin())
	}
	if got := ctx.Query("secret"); got != "" {
		t.Fatalf("Query on nil request = %q, want empty", got)
	}
	if got := ctx.BearerToken(); got != "" {
		t.Fatalf("BearerToken on nil request = %q, want empty", got)
	}
	bearerReq := httptest.NewRequest(http.MethodGet, "/", nil)
	bearerReq.Header.Set("Authorization", "bearer   token-1")
	if got := NewContext(bearerReq).BearerToken(); got != "token-1" {
		t.Fatalf("BearerToken valid header = %q, want token-1", got)
	}
	for _, header := range []string{
		"Bearer",
		"Bearer token extra-secret",
		"Basic token",
		"Bearer " + strings.Repeat("x", maxBearerTokenHeaderBytes),
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", header)
		if got := NewContext(req).BearerToken(); got != "" {
			t.Fatalf("BearerToken malformed header %q = %q, want empty", header, got)
		}
	}
	if got := ctx.ClientIP(); got != "" {
		t.Fatalf("ClientIP on nil request = %q, want empty", got)
	}
	if _, err := ctx.Cookie("session_id"); err != http.ErrNoCookie {
		t.Fatalf("Cookie on nil request err = %v, want http.ErrNoCookie", err)
	}

	var nilCtx *Context
	if got := nilCtx.Query("secret"); got != "" {
		t.Fatalf("Query on nil context = %q, want empty", got)
	}
	if got := nilCtx.BearerToken(); got != "" {
		t.Fatalf("BearerToken on nil context = %q, want empty", got)
	}
	if got := nilCtx.ClientIP(); got != "" {
		t.Fatalf("ClientIP on nil context = %q, want empty", got)
	}
	if _, err := nilCtx.Cookie("session_id"); err != http.ErrNoCookie {
		t.Fatalf("Cookie on nil context err = %v, want http.ErrNoCookie", err)
	}
	if nilCtx.Request() != nil {
		t.Fatal("Request on nil context should return nil")
	}
	if nilCtx.ResponseWriter() != nil {
		t.Fatal("ResponseWriter on nil context should return nil")
	}
	if nilCtx.Auth().UserID != "" {
		t.Fatalf("Auth on nil context = %#v, want empty auth info", nilCtx.Auth())
	}
	if got := nilCtx.Param("missing"); got != "" {
		t.Fatalf("Param on nil context = %q, want empty", got)
	}
	nilCtx.SetParam("id", "123")
	nilCtx.SetAuth(&AuthInfo{UserID: "user-1"})
	nilCtx.SetAuth(nil)
	nilCtx.SetRoles([]RoleInfo{{Slug: "admin", Manages: true}})
	if nilCtx.IsAuthenticated() {
		t.Fatal("nil context should not be authenticated")
	}
	if got := nilCtx.Role(); got != "" {
		t.Fatalf("Role on nil context = %q, want empty", got)
	}
	if got := nilCtx.Roles(); got != nil {
		t.Fatalf("Roles on nil context = %#v, want nil", got)
	}
	if nilCtx.HasRole("admin") || nilCtx.HasAnyRole("admin", "editor") || nilCtx.IsAdmin() {
		t.Fatal("nil context role checks should fail closed")
	}
}

func TestExportedRateLimitEventsHandleNilRequestContext(t *testing.T) {
	resetTrustedProxyStateForTest()
	previous := rateLimitCallback
	defer func() { rateLimitCallback = previous }()

	var events []RateLimitEvent
	rateLimitCallback = func(ctx *Context, event RateLimitEvent) {
		events = append(events, event)
	}
	limiter := RateLimit(1, 1)
	resp := limiter(NewContext(nil), func() Response {
		return Response{StatusCode: http.StatusNoContent}
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("rate limit response status = %d", resp.StatusCode)
	}
	if len(events) != 1 {
		t.Fatalf("rate limit events = %d, want 1", len(events))
	}
	if events[0].Path != "" || events[0].Key != "" {
		t.Fatalf("nil request event = %#v, want empty path and key", events[0])
	}

	events = nil
	nilContextLimiter := RateLimit(1, 1)
	resp = nilContextLimiter(nil, func() Response {
		return Response{StatusCode: http.StatusAccepted}
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("nil context rate limit response status = %d", resp.StatusCode)
	}
	if len(events) != 1 {
		t.Fatalf("nil context rate limit events = %d, want 1", len(events))
	}
	if events[0].Path != "" || events[0].Key != "" {
		t.Fatalf("nil context event = %#v, want empty path and key", events[0])
	}
}

func TestExportedAuthRateLimitHandlesNilContext(t *testing.T) {
	resetTrustedProxyStateForTest()
	previous := rateLimitCallback
	defer func() { rateLimitCallback = previous }()

	var events []RateLimitEvent
	rateLimitCallback = func(ctx *Context, event RateLimitEvent) {
		events = append(events, event)
	}
	limiter := AuthRateLimit().RPS(1).Burst(1)
	resp := limiter.Middleware()(nil, func() Response {
		return Response{StatusCode: http.StatusNoContent}
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("auth rate limit response status = %d", resp.StatusCode)
	}
	if len(events) != 1 {
		t.Fatalf("auth rate limit events = %d, want 1", len(events))
	}
	if events[0].Layer != "auth" || events[0].Path != "" || events[0].Key != "" {
		t.Fatalf("nil context auth rate limit event = %#v", events[0])
	}
}

func TestExportedRateLimitMiddlewareNilNextFailsClosed(t *testing.T) {
	resetTrustedProxyStateForTest()
	t.Setenv("RATE_LIMIT", "false")

	for name, resp := range map[string]Response{
		"ip":       RateLimit(1, 1)(NewContext(requestFrom("192.0.2.10:1234", "")), nil),
		"disabled": RateLimit(0, 1)(NewContext(requestFrom("192.0.2.11:1234", "")), nil),
		"auth":     AuthRateLimit().RPS(1).Burst(1).Middleware()(NewContext(requestFrom("192.0.2.12:1234", "")), nil),
	} {
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", name, resp.StatusCode)
		}
		body, ok := resp.Body.(map[string]string)
		if !ok || body["error"] != "internal server error" {
			t.Fatalf("%s body = %#v, want sanitized internal server error", name, resp.Body)
		}
		if strings.Contains(body["error"], "panic") || strings.Contains(body["error"], "nil pointer") || strings.Contains(body["error"], "192.0.2") {
			t.Fatalf("%s leaked internal detail: %#v", name, body)
		}
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
	"strings"
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

func TestExportedLoadRolesDatabaseErrorsAreSanitized(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := httpx.NewContext(newRequest())
	ctx.SetAuth(&httpx.AuthInfo{UserID: "user-1"})
	resp := middleware.LoadRoles(ctx, func() httpx.Response {
		t.Fatal("LoadRoles should not call next after database failure")
		return httpx.Response{}
	})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body, ok := resp.Body.(map[string]string)
	if !ok {
		t.Fatalf("body type = %T", resp.Body)
	}
	if body["error"] != "internal server error" {
		t.Fatalf("body = %#v, want sanitized internal error", body)
	}
	if strings.Contains(body["error"], "sql") || strings.Contains(body["error"], "closed") {
		t.Fatalf("RBAC database error leaked detail: %#v", body)
	}
}

func TestExportedRBACMiddlewareNilNextFailsClosed(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := policies.Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}
	ctx := httpx.NewContext(newRequest())
	ctx.SetAuth(&httpx.AuthInfo{UserID: "user-1", Role: "admin"})

	for name, resp := range map[string]httpx.Response{
		"LoadRoles":   middleware.LoadRoles(ctx, nil),
		"RequireRole": middleware.RequireRole("admin")(ctx, nil),
		"RequireAdmin": middleware.RequireAdmin(ctx, nil),
	} {
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", name, resp.StatusCode)
		}
		body, ok := resp.Body.(map[string]string)
		if !ok || body["error"] != "internal server error" {
			t.Fatalf("%s body = %#v, want sanitized internal server error", name, resp.Body)
		}
		for _, forbidden := range []string{"panic", "nil pointer", "roles", "admin", "user-1"} {
			if strings.Contains(body["error"], forbidden) {
				t.Fatalf("%s leaked %q in body %#v", name, forbidden, body)
			}
		}
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

func writeVisibilityQuerySourceFixture(t *testing.T, projectDir string) {
	t.Helper()
	postMigration := filepath.Join(projectDir, "database", "migrations", "2026_02_21_100001_create_posts_table.go")
	data, err := os.ReadFile(postMigration)
	if err != nil {
		t.Fatal(err)
	}
	rewritten := strings.Replace(string(data), `t.Text("body").NotNull().OwnerSees()`, `t.Text("body").NotNull().OwnerSees().RoleSees("editor")`, 1)
	if rewritten == string(data) {
		t.Fatal("post migration fixture did not contain owner-visible body column")
	}
	if err := os.WriteFile(postMigration, []byte(rewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `package services

import models "github.com/shortontech/pickle/testdata/basic-crud/app/models"

func PublicUsersByName(name string) ([]models.User, error) {
	return models.QueryUser().
		SelectPublic().
		WhereName(name).
		All()
}

func OwnerPostsByStatus(status string) ([]models.Post, error) {
	q := models.QueryPost()
	q.SelectOwner()
	q.WhereStatus(status)
	return q.All()
}

func RolePostsByStatus(status string, roles []string) ([]models.Post, error) {
	return models.QueryPost().
		SelectForRoles(roles).
		WhereStatus(status).
		All()
}

func OwnerRolePostsByStatus(status string, roles []string) ([]models.Post, error) {
	q := models.QueryPost()
	q.SelectForOwner(roles)
	q.WhereStatus(status)
	return q.All()
}
`
	if err := os.WriteFile(filepath.Join(projectDir, "app", "services", "visibility_selectors.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestCommand(t *testing.T, projectDir string) {
	t.Helper()
	dir := filepath.Join(projectDir, "app", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package commands

import "os"

type AuditMarkerCommand struct{}

func (c AuditMarkerCommand) Name() string { return "audit:marker" }
func (c AuditMarkerCommand) Description() string { return "Write audit marker" }
func (c AuditMarkerCommand) Run(args []string) error {
	return os.WriteFile(os.Getenv("AUDIT_COMMAND_MARKER"), []byte("ran"), 0o600)
}
`
	shadow := `package commands

import "errors"

type ShadowMigrateCommand struct{}

func (c ShadowMigrateCommand) Name() string { return "migrate" }
func (c ShadowMigrateCommand) Description() string { return "Shadow built-in migrate" }
func (c ShadowMigrateCommand) Run(args []string) error {
	return errors.New("shadow migrate should not run")
}
`
	if err := os.WriteFile(filepath.Join(dir, "audit_marker.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shadow_migrate.go"), []byte(shadow), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestScope(t *testing.T, projectDir, modelDir, filename, src string) {
	t.Helper()
	dir := filepath.Join(projectDir, "database", "scopes", modelDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTestService(t *testing.T, projectDir, filename, src string) {
	t.Helper()
	dir := filepath.Join(projectDir, "app", "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedCustomScopeBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"encryption-test/app/models"
)

func TestExportedCustomScopeFiltersWithStandaloneQuerySupport(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.Session{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []models.Session{
		{ID: "admin", UserID: uuid.New(), Role: "admin", ExpiresAt: now},
		{ID: "viewer", UserID: uuid.New(), Role: "viewer", ExpiresAt: now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	sessions, err := models.QuerySession().Admin().ExpiresAfter(now.Add(-time.Minute)).All()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "admin" {
		t.Fatalf("Admin scope returned %+v, want only admin session", sessions)
	}

	err = models.WithTransaction(func(tx *models.Tx) error {
		txSessions, err := tx.QuerySession().Admin().ExpiresAfter(now.Add(-time.Minute)).All()
		if err != nil {
			return err
		}
		if len(txSessions) != 1 || txSessions[0].ID != "admin" {
			t.Fatalf("transaction Admin scope returned %+v, want only admin session", txSessions)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	rolledBackID := "rolled-back"
	rollbackErr := errors.New("force rollback")
	err = models.WithTransaction(func(tx *models.Tx) error {
		if err := tx.QuerySession().Create(&models.Session{
			ID:        rolledBackID,
			UserID:    uuid.New(),
			Role:      "admin",
			ExpiresAt: now,
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback transaction error = %v, want %v", err, rollbackErr)
	}
	var rolledBackCount int64
	if err := db.Model(&models.Session{}).Where("id = ?", rolledBackID).Count(&rolledBackCount).Error; err != nil {
		t.Fatal(err)
	}
	if rolledBackCount != 0 {
		t.Fatalf("tx.QuerySession().Create escaped rollback; count = %d", rolledBackCount)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_custom_scope_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedEncryptionBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models_test

import (
	"encoding/base64"
	"os"
	"strings"
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
	} else if err.Error() != "decryption error" || strings.Contains(err.Error(), "cipher") || strings.Contains(err.Error(), tampered) {
		t.Fatalf("tampered sealed ciphertext error = %v, want sanitized decryption error", err)
	}

	badDeterministic := "not-valid-base64-secret"
	if err := db.Model(&models.User{}).Where("id = ?", user.ID).Update("email_encrypted", badDeterministic).Error; err != nil {
		t.Fatalf("tamper deterministic field: %v", err)
	}
	var brokenDeterministic models.User
	if err := db.First(&brokenDeterministic, "id = ?", user.ID).Error; err == nil {
		t.Fatal("malformed deterministic ciphertext should fail")
	} else if err.Error() != "decryption error" || strings.Contains(err.Error(), "base64") || strings.Contains(err.Error(), "illegal") || strings.Contains(err.Error(), badDeterministic) {
		t.Fatalf("malformed deterministic ciphertext error = %v, want sanitized decryption error", err)
	}

	os.Unsetenv("APP_ENCRYPTION_KEY")
}

func TestExportedEncryptedFilterErrorsAreSanitized(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	t.Setenv("APP_ENCRYPTION_KEY", key)

	_, err := models.EncryptDeterministicFilterValue(secretFilterValue{})
	if err == nil || err.Error() != "encrypted filter value must be a string or []string" {
		t.Fatalf("invalid scalar encrypted filter error = %v, want sanitized scalar error", err)
	}
	if err != nil && (strings.Contains(err.Error(), "secretFilterValue") || strings.Contains(err.Error(), "models_test")) {
		t.Fatalf("invalid scalar encrypted filter leaked type detail: %v", err)
	}

	_, err = models.EncryptDeterministicFilterValue([]any{"ok", secretFilterValue{}})
	if err == nil || err.Error() != "encrypted filter values must be strings" {
		t.Fatalf("invalid list encrypted filter error = %v, want sanitized list error", err)
	}
	if err != nil && (strings.Contains(err.Error(), "secretFilterValue") || strings.Contains(err.Error(), "models_test")) {
		t.Fatalf("invalid list encrypted filter leaked type detail: %v", err)
	}
}

func TestExportedEncryptedColumnsFailClosedWithoutValidKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "missing", key: ""},
		{name: "too short", key: "short-secret-key"},
		{name: "bad base64 length", key: base64.StdEncoding.EncodeToString([]byte("too-short"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENCRYPTION_KEY", tc.key)
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
			err = db.Create(user).Error
			if err == nil {
				t.Fatal("create with invalid encryption key should fail")
			}
			for _, leak := range []string{tc.key, "ada@example.com", "api-secret", "private-secret"} {
				if leak != "" && strings.Contains(err.Error(), leak) {
					t.Fatalf("invalid key error leaked %q: %v", leak, err)
				}
			}
			var rows int64
			if err := db.Model(&models.User{}).Count(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if rows != 0 {
				t.Fatalf("rows after invalid encryption key create = %d, want 0", rows)
			}
		})
	}
}

type secretFilterValue struct{}
	`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_encryption_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedIntegrityBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models_test

import (
	"strings"
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

func TestExportedIntegrityDatabaseErrorsAreSanitized(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.Account{}, &models.Transaction{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	tx := &models.Transaction{AccountID: uuid.New(), Type: "credit", Amount: decimal.NewFromInt(100), Currency: "USD"}
	for name, err := range map[string]error{
		"create": models.CreateTransaction(tx),
		"verify": models.VerifyTransactionChain(),
	} {
		if err == nil {
			t.Fatalf("%s should fail with closed database", name)
		}
		if err.Error() != "integrity database error" {
			t.Fatalf("%s error = %v, want sanitized integrity database error", name, err)
		}
		for _, forbidden := range []string{"sql:", "closed", "transactions", "SELECT", "password"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("%s error leaked %q: %v", name, forbidden, err)
			}
		}
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
	writeLedgerIntegrityQuerySourceFixture(t, projectDir)
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
	assertFileContains(t, filepath.Join(out, "app", "models", "integrity_support.go"), "func integrityDatabaseError() error")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "integrity_support.go"), "return db.Create(record).Error")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "integrity_verify.go"), "models.VerifyTransactionRow(tx)")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "integrity_verify.go"), "models.VerifyTransactionChain()")
	assertFileNotContains(t, filepath.Join(out, "app", "http", "controllers", "integrity_verify.go"), "QueryTransaction")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "account_controller.go"), "models.DB.Model(&models.")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "account_controller.go"), "Account{})")
	assertPathMissing(t, filepath.Join(out, "integrity_test.go"))
	assertCleanExportReport(t, out)
	assertStandaloneNoPickleRuntime(t, out)
	assertNoGoFileContains(t, out, "QueryAccount")
	writeExportedIntegrityBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeLedgerIntegrityQuerySourceFixture(t *testing.T, projectDir string) {
	t.Helper()
	src := `package controllers

import "github.com/shortontech/ledger/app/models"

func VerifyTransactionIntegrity(tx *models.Transaction) error {
	if err := models.QueryTransaction().VerifyRow(tx); err != nil {
		return err
	}
	q := models.QueryTransaction()
	return q.VerifyChain()
}
`
	path := filepath.Join(projectDir, "app", "http", "controllers", "integrity_verify.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
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
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedEncryptionBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportCustomScopesEmitStandaloneQuerySupport(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "encryption-test"))
	writeTestScope(t, projectDir, "session", "admin.go", `package session

import (
	"time"

	"github.com/shortontech/pickle/testdata/encryption-test/app/models"
)

func Admin(q *models.SessionScopeBuilder) *models.SessionScopeBuilder {
	return q.WhereRole("admin").OrderBy("id", "ASC")
}

func ExpiresAfter(q *models.SessionScopeBuilder, since time.Time) *models.SessionScopeBuilder {
	return q.WhereExpiresAtAfter(since)
}
`)
	writeTestService(t, projectDir, "scoped_tx.go", `package services

import (
	"time"

	"github.com/shortontech/pickle/testdata/encryption-test/app/models"
)

func CountAdminSessionsInTransaction() (int, error) {
	count := 0
	err := models.WithTransaction(func(tx *models.Tx) error {
		sessions, err := tx.QuerySession().Admin().ExpiresAfter(time.Now().Add(-time.Hour)).All()
		if err != nil {
			return err
		}
		count = len(sessions)
		return nil
	})
	return count, err
}
`)
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
	if len(res.Findings) != 0 {
		t.Fatalf("did not expect findings, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "models", "query_support.go"), "type SessionScopeBuilder struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "query_support.go"), "func QuerySession() *SessionQuery")
	assertFileContains(t, filepath.Join(out, "app", "models", "query_support.go"), "func (tx *Tx) QuerySession() *SessionQuery")
	assertFileContains(t, filepath.Join(out, "app", "models", "query_support.go"), "func (q *SessionQuery) Create(record *Session) error {")
	assertFileContains(t, filepath.Join(out, "app", "models", "query_support.go"), "return q.db.Session(&gorm.Session{NewDB: true}).Create(record).Error")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "query_support.go"), "return DB.Create(record).Error")
	assertFileContains(t, filepath.Join(out, "app", "models", "query_support.go"), "func (sb *SessionScopeBuilder) WhereRole(value any) *SessionScopeBuilder")
	assertFileContains(t, filepath.Join(out, "app", "models", "custom_scopes_gen.go"), "func (q *SessionQuery) Admin() *SessionQuery")
	assertFileContains(t, filepath.Join(out, "app", "models", "custom_scopes_gen.go"), "func (q *SessionQuery) ExpiresAfter(since time.Time) *SessionQuery")
	assertFileContains(t, filepath.Join(out, "app", "models", "custom_scopes_gen.go"), "func sessionScopeAdmin(q *SessionScopeBuilder) *SessionScopeBuilder")
	assertFileContains(t, filepath.Join(out, "app", "models", "custom_scopes_gen.go"), `"time"`)
	assertFileContains(t, filepath.Join(out, "app", "services", "scoped_tx.go"), "tx.QuerySession().Admin().ExpiresAfter")
	assertFileContains(t, filepath.Join(out, "database", "scopes", "session", "admin.go"), `"encryption-test/app/models"`)
	assertPathMissing(t, filepath.Join(out, "gqlgen.yml"))
	assertFileNotContains(t, filepath.Join(out, "go.mod"), "github.com/99designs/gqlgen")
	assertCleanExportReport(t, out)
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedCustomScopeBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportZeroGraphQLLowersToGQLGenTarget(t *testing.T) {
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

	assertFileContains(t, filepath.Join(out, "go.mod"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "schema:")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "- app/graphqlapi/schema.graphqls")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/generated/generated.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: generated")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/model/models_gen.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "dir: app/graphqlapi/resolver")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: resolver")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), "//go:build tools")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), `_ "github.com/99designs/gqlgen"`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func Handler() http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func PlaygroundHandler(endpoint string) http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "generated.NewExecutableSchema")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "Auth:        graphQLAPIAuthDirective")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "extension.FixedComplexityLimit")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "Complexity: graphQLAPIComplexityRoot()")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIOperations = 1")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), "root.User.Posts = func(childComplexity int) int")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), "graphQLAPIListComplexity(childComplexity, 1, min(100, maxGraphQLAPIComplexityPageSize))")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), `if contentType == "" {
		return false
	}`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "generated", "generated.go"), "type ResolverRoot interface")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *Resolver) Query() generated.QueryResolver")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), "func gqlgenPageWindow")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), `graphQLAPIBadInput("page.first and page.last cannot both be set")`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), `graphQLAPIBadInput("post: invalid id")`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), `graphQLAPIBadInput("invalid GraphQL ID input")`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "TotalCount: totalCount")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "TotalCount: len(edges)")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: Users")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: User")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: Posts")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: Post")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: Comments")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: Comment")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: CreateUser")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: UpdateUser")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: DeleteUser")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: CreatePost")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: UpdatePost")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: DeletePost")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: CreateComment")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: UpdateComment")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "not implemented: DeleteComment")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "type Query")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "type User")
	assertNoGoFilesUnder(t, filepath.Join(out, "app", "graphql"))
	assertNoGoFileContains(t, filepath.Join(out, "app", "graphql"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func QueryUser() *UserQuery")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "EMAIL_ASC")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "EMAIL_DESC")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `case "email":`)
	assertFileNotContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `column = "email_encrypted"`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "commands.NewApp().Run(os.Args[1:])")
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), `mux.Handle("/graphql", graphqlapi.Handler())`)
	assertFileContains(t, filepath.Join(out, "app", "commands", "support.go"), "routes.API.RegisterRoutes(mux)")
	assertFileNotContains(t, filepath.Join("..", "..", "go.mod"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Exported Go GraphQL target backed by gqlgen")
	assertFileNotContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Generated GraphQL package")
	assertFileContains(t, filepath.Join(out, "app", "http", "requests", "bindings.go"), "package requests")
	assertFileContains(t, filepath.Join(out, "internal", "httpx", "httpx.go"), "func writeRouterNotFound")
	assertCleanExportReport(t, out)
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedZeroGraphQLEncryptedFilterTest(t, out)
	writeExportedZeroGraphQLAPITargetBehaviorTest(t, out)
	writeExportedZeroGraphQLAPIHTTPBehaviorTest(t, out)
	writeExportedZeroGraphQLAPIErrorBehaviorTest(t, out)
	writeExportedZeroGraphQLRouteTargetBehaviorTest(t, out)
	writeExportedZeroGraphQLAPIComplexityBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedZeroGraphQLAPIComplexityBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphqlapi

import (
	"testing"

	"zero-graphql/app/graphqlapi/model"
)

func TestExportedGQLGenTargetDefaultRelationshipComplexityIsUsableAndBounded(t *testing.T) {
	root := graphQLAPIComplexityRoot()
	if root.User.Posts == nil {
		t.Fatal("User.posts complexity hook was not generated")
	}
	if root.Post.Comments == nil {
		t.Fatal("Post.comments complexity hook was not generated")
	}

	if got, want := root.User.Posts(1), 200; got != want {
		t.Fatalf("User.posts default complexity = %d, want %d", got, want)
	}
	if got := root.User.Posts(root.Post.Comments(1)); got <= maxGraphQLAPIComplexity {
		t.Fatalf("nested relationship complexity = %d, want above max %d", got, maxGraphQLAPIComplexity)
	}
}

func TestExportedGQLGenTargetDefaultTopLevelComplexityIsPageBounded(t *testing.T) {
	root := graphQLAPIComplexityRoot()
	if root.Query.Users == nil {
		t.Fatal("Query.users complexity hook was not generated")
	}

	first := 10
	if got, want := root.Query.Users(1, nil, nil, &model.PageInput{First: &first}), 20; got != want {
		t.Fatalf("Query.users complexity = %d, want %d", got, want)
	}
	first = 101
	if got := root.Query.Users(0, nil, nil, &model.PageInput{First: &first}); got <= maxGraphQLAPIComplexity {
		t.Fatalf("oversized Query.users page complexity = %d, want above max %d", got, maxGraphQLAPIComplexity)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "exported_complexity_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedZeroGraphQLEncryptedFilterTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package models

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/gqlerror"
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

	if _, err := QueryUser().WhereEmailLike("ada%").First(); !isBadInput(err) {
		t.Fatalf("unsupported encrypted GraphQL filter error = %v, want BAD_USER_INPUT", err)
	}

	t.Setenv("APP_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	if _, err := graphQLEncryptFilterValue(42); !isBadInput(err) {
		t.Fatalf("invalid encrypted GraphQL filter scalar error = %v, want BAD_USER_INPUT", err)
	} else if strings.Contains(err.Error(), "int") || strings.Contains(err.Error(), "%!") {
		t.Fatalf("invalid encrypted GraphQL filter scalar leaked type detail: %v", err)
	}
	if _, err := graphQLEncryptFilterValue([]any{"ada@example.com", 42}); !isBadInput(err) {
		t.Fatalf("invalid encrypted GraphQL filter list error = %v, want BAD_USER_INPUT", err)
	} else if strings.Contains(err.Error(), "int") || strings.Contains(err.Error(), "%!") {
		t.Fatalf("invalid encrypted GraphQL filter list leaked type detail: %v", err)
	}
}

func isBadInput(err error) bool {
	gqlErr, ok := err.(*gqlerror.Error)
	return ok && gqlErr.Extensions["code"] == "BAD_USER_INPUT"
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "models", "exported_graphql_encrypted_filter_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedZeroGraphQLAPITargetBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"zero-graphql/app/graphqlapi/model"
	"zero-graphql/app/models"
)

func TestExportedGQLGenTargetCRUDResolvers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.Post{}, &models.Comment{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	ctx := context.Background()
	resolver := &Resolver{}
	mutations := &mutationResolver{Resolver: resolver}
	queries := &queryResolver{Resolver: resolver}

	userID := uuid.New()
	if _, err := mutations.CreatePost(ctx, model.CreatePostInput{
		UserID: userID.String(),
		Title:  "Nope",
		Body:   "unauthenticated",
	}); err == nil {
		t.Fatal("direct create post without auth should fail")
	}
	ctx = WithGraphQLAPIAuthClaims(ctx, &GraphQLAPIAuthClaims{UserID: userID.String(), Role: "admin"})
	badID := "not-a-uuid"
	spoofedID := uuid.New()
	post, err := mutations.CreatePost(ctx, model.CreatePostInput{
		UserID: spoofedID.String(),
		Title:  "Hello",
		Body:   "GraphQL body",
		Status: stringPtr("draft"),
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if post.ID == uuid.Nil || post.UserID != userID || post.Title != "Hello" || post.Status != "draft" {
		t.Fatalf("created post = %+v", post)
	}
	if post.UserID == spoofedID {
		t.Fatalf("create post trusted GraphQL owner input: %+v", post)
	}
	if post.CreatedAt.IsZero() || post.UpdatedAt.IsZero() {
		t.Fatalf("post timestamps were not initialized: %+v", post)
	}
	for _, title := range []string{"Second", "Third"} {
		if _, err := mutations.CreatePost(ctx, model.CreatePostInput{
			UserID: userID.String(),
			Title:  title,
			Body:   "GraphQL body",
			Status: stringPtr("draft"),
		}); err != nil {
			t.Fatalf("create extra post %q: %v", title, err)
		}
	}
	pagedPosts, err := queries.Posts(ctx, nil, nil, &model.PageInput{First: intPtr(1)})
	if err != nil {
		t.Fatalf("paged posts: %v", err)
	}
	if len(pagedPosts.Edges) != 1 {
		t.Fatalf("paged posts edges = %d, want 1", len(pagedPosts.Edges))
	}
	if pagedPosts.TotalCount != 3 {
		t.Fatalf("paged posts totalCount = %d, want all matching rows 3", pagedPosts.TotalCount)
	}
	if pagedPosts.PageInfo == nil || !pagedPosts.PageInfo.HasNextPage {
		t.Fatalf("paged posts pageInfo = %+v, want hasNextPage", pagedPosts.PageInfo)
	}
	if pagedPosts.PageInfo.EndCursor == nil || *pagedPosts.PageInfo.EndCursor != "cursor:0" {
		t.Fatalf("paged posts endCursor = %v, want cursor:0", pagedPosts.PageInfo.EndCursor)
	}
	afterPosts, err := queries.Posts(ctx, nil, nil, &model.PageInput{First: intPtr(1), After: pagedPosts.PageInfo.EndCursor})
	if err != nil {
		t.Fatalf("after posts: %v", err)
	}
	if len(afterPosts.Edges) != 1 || afterPosts.Edges[0].Cursor != "cursor:1" {
		t.Fatalf("after posts edges = %+v, want first edge cursor:1", afterPosts.Edges)
	}
	if afterPosts.PageInfo == nil || !afterPosts.PageInfo.HasPreviousPage || !afterPosts.PageInfo.HasNextPage {
		t.Fatalf("after posts pageInfo = %+v, want previous and next page", afterPosts.PageInfo)
	}
	beforeThird := "cursor:2"
	backwardPosts, err := queries.Posts(ctx, nil, nil, &model.PageInput{Last: intPtr(1), Before: &beforeThird})
	if err != nil {
		t.Fatalf("backward posts: %v", err)
	}
	if len(backwardPosts.Edges) != 1 || backwardPosts.Edges[0].Cursor != "cursor:1" {
		t.Fatalf("backward posts edges = %+v, want cursor:1", backwardPosts.Edges)
	}
	if backwardPosts.PageInfo == nil || !backwardPosts.PageInfo.HasPreviousPage || !backwardPosts.PageInfo.HasNextPage {
		t.Fatalf("backward posts pageInfo = %+v, want previous and next page", backwardPosts.PageInfo)
	}

	posts, err := queries.Posts(ctx, &model.PostFilter{ID: &model.IDFilter{Eq: stringPtr(post.ID.String())}}, nil, &model.PageInput{First: intPtr(1)})
	if err != nil {
		t.Fatalf("query posts: %v", err)
	}
	if len(posts.Edges) != 1 || posts.Edges[0].Node.ID != post.ID {
		t.Fatalf("posts connection = %+v", posts)
	}
	if posts.TotalCount != 1 {
		t.Fatalf("filtered posts totalCount = %d, want 1", posts.TotalCount)
	}
	tooManyIDs := make([]string, maxGraphQLAPIInputListSize+1)
	for i := range tooManyIDs {
		tooManyIDs[i] = uuid.NewString()
	}
	if _, err := queries.Posts(ctx, &model.PostFilter{ID: &model.IDFilter{In: tooManyIDs}}, nil, nil); err == nil {
		t.Fatal("oversized id filter should fail")
	}
	if _, err := queries.Posts(ctx, &model.PostFilter{ID: &model.IDFilter{Eq: &badID}}, nil, nil); !isBadInput(err) {
		t.Fatalf("invalid id filter error = %v, want BAD_USER_INPUT", err)
	}
	if _, err := queries.Posts(ctx, &model.PostFilter{ID: &model.IDFilter{In: []string{uuid.NewString(), badID}}}, nil, nil); !isBadInput(err) {
		t.Fatalf("invalid id list filter error = %v, want BAD_USER_INPUT", err)
	}
	tooLargePage := maxGraphQLAPIPageSize + 1
	if _, err := queries.Posts(ctx, nil, nil, &model.PageInput{First: &tooLargePage}); !isBadInput(err) {
		t.Fatalf("oversized page error = %v, want BAD_USER_INPUT", err)
	}
	if _, err := queries.Posts(ctx, nil, nil, &model.PageInput{First: intPtr(1), Last: intPtr(1)}); !isBadInput(err) {
		t.Fatalf("first+last page error = %v, want BAD_USER_INPUT", err)
	}
	zeroPage := 0
	if _, err := queries.Posts(ctx, nil, nil, &model.PageInput{First: &zeroPage}); !isBadInput(err) {
		t.Fatalf("zero page size error = %v, want BAD_USER_INPUT", err)
	}
	tooManyTitles := make([]string, maxGraphQLAPIInputListSize+1)
	for i := range tooManyTitles {
		tooManyTitles[i] = "title"
	}
	if _, err := queries.Posts(ctx, &model.PostFilter{Title: &model.StringFilter{In: tooManyTitles}}, nil, nil); !isBadInput(err) {
		t.Fatalf("oversized string filter error = %v, want BAD_USER_INPUT", err)
	}
	badCursor := "not-a-cursor"
	if _, err := queries.Posts(ctx, nil, nil, &model.PageInput{After: &badCursor}); !isBadInput(err) {
		t.Fatalf("invalid cursor error = %v, want BAD_USER_INPUT", err)
	}
	if _, err := queries.Post(ctx, badID); !isBadInput(err) {
		t.Fatalf("invalid single id error = %v, want BAD_USER_INPUT", err)
	}
	if _, err := mutations.UpdatePost(ctx, badID, model.UpdatePostInput{Title: stringPtr("Bad ID")}); !isBadInput(err) {
		t.Fatalf("invalid update id error = %v, want BAD_USER_INPUT", err)
	}
	if _, err := mutations.DeletePost(ctx, badID); !isBadInput(err) {
		t.Fatalf("invalid delete id error = %v, want BAD_USER_INPUT", err)
	}
	strangerCtx := WithGraphQLAPIAuthClaims(context.Background(), &GraphQLAPIAuthClaims{UserID: uuid.NewString(), Role: "admin"})
	if _, err := mutations.UpdatePost(strangerCtx, post.ID.String(), model.UpdatePostInput{Title: stringPtr("Stolen")}); err == nil {
		t.Fatal("stranger update should be owner-scoped")
	}
	if deleted, err := mutations.DeletePost(strangerCtx, post.ID.String()); err == nil || deleted {
		t.Fatalf("stranger delete = %v, %v; want owner-scoped denial", deleted, err)
	}

	updated, err := mutations.UpdatePost(ctx, post.ID.String(), model.UpdatePostInput{Title: stringPtr("Updated")})
	if err != nil {
		t.Fatalf("update post: %v", err)
	}
	if updated.Title != "Updated" || updated.UserID != userID {
		t.Fatalf("updated post = %+v", updated)
	}
	if updated.UpdatedAt.Before(post.UpdatedAt) {
		t.Fatalf("updated_at moved backwards: before=%s after=%s", post.UpdatedAt.Format(time.RFC3339Nano), updated.UpdatedAt.Format(time.RFC3339Nano))
	}

	comment, err := mutations.CreateComment(ctx, model.CreateCommentInput{
		PostID: post.ID.String(),
		UserID: spoofedID.String(),
		Body:   "Nice post",
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if comment.ID == uuid.Nil || comment.PostID != post.ID || comment.UserID != userID {
		t.Fatalf("created comment = %+v", comment)
	}
	deletedComment, err := mutations.DeleteComment(ctx, comment.ID.String())
	if err != nil || !deletedComment {
		t.Fatalf("delete comment = %v, %v", deletedComment, err)
	}
	deletedPost, err := mutations.DeletePost(ctx, post.ID.String())
	if err != nil || !deletedPost {
		t.Fatalf("delete post = %v, %v", deletedPost, err)
	}
}

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }

func isBadInput(err error) bool {
	gqlErr, ok := err.(*gqlerror.Error)
	return ok && gqlErr.Extensions["code"] == "BAD_USER_INPUT"
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "resolver", "exported_target_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedZeroGraphQLAPIHTTPBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphqlapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"zero-graphql/app/graphqlapi"
	"zero-graphql/app/http/auth"
	"zero-graphql/app/http/auth/jwt"
	"zero-graphql/app/models"
)

func TestExportedGQLGenTargetHandlerEnforcesAuthDirective(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.Post{}, &models.JwtToken{}); err != nil {
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

	query := ` + "`" + `mutation CreatePost($input: CreatePostInput!) {
  createPost(input: $input) { id title userId }
}` + "`" + `
	body := []byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(query) + ` + "`" + `,"variables":{"input":{"userId":"` + "`" + ` + uuid.NewString() + ` + "`" + `","title":"Target","body":"via gqlgen","status":"draft"}}}` + "`" + `)
	handler := graphqlapi.Handler()

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !responseHasErrorCode(t, rec.Body.Bytes(), "UNAUTHENTICATED") {
		t.Fatalf("unauthenticated mutation should be denied, body=%s", rec.Body.String())
	}
	var count int64
	if err := db.Model(&models.Post{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unauthenticated mutation inserted %d posts", count)
	}

	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{
		Subject:   uuid.NewString(),
		Role:      "admin",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			CreatePost struct {
				ID    string ` + "`" + `json:"id"` + "`" + `
				Title string ` + "`" + `json:"title"` + "`" + `
			} ` + "`" + `json:"createPost"` + "`" + `
		} ` + "`" + `json:"data"` + "`" + `
		Errors []map[string]any ` + "`" + `json:"errors"` + "`" + `
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode authenticated response: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Errors) != 0 || resp.Data.CreatePost.ID == "" || resp.Data.CreatePost.Title != "Target" {
		t.Fatalf("authenticated mutation response = %s", rec.Body.String())
	}
}

func TestExportedGQLGenTargetCreateUserRequiresInternalFields(t *testing.T) {
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
		Subject:   uuid.NewString(),
		Role:      "admin",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	body := []byte(` + "`" + `{"query":"mutation { createUser(input: { name: \"Ada\", email: \"ada@example.com\" }) { id name } }"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	graphqlapi.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "required internal field") {
		t.Fatalf("createUser should fail closed for required internal fields: %s", rec.Body.String())
	}
	var count int64
	if err := db.Model(&models.User{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("createUser inserted %d users with missing password hash", count)
	}
}

func TestExportedGQLGenTargetHandlerRejectsUnsafeRequests(t *testing.T) {
	handler := graphqlapi.Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, nil)
	if rec.Code != http.StatusBadRequest || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("nil request response status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "panic") || strings.Contains(rec.Body.String(), "nil pointer") {
		t.Fatalf("nil request response leaked panic detail: %s", rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/graphql?query={posts{totalCount}}", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader([]byte(` + "`" + `{"query":123}` + "`" + `)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("bad query response status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader([]byte(` + "`" + `{"query":"{ posts { totalCount } }"}` + "`" + `)))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("missing content type response status=%d body=%s", rec.Code, rec.Body.String())
	}
	oversizedBody := bytes.Repeat([]byte("x"), (1<<20)+1)
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(oversizedBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("oversized body response status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "graphql request body too large") {
		t.Fatalf("oversized body response should name body limit without internals: %s", rec.Body.String())
	}
	oversizedQuery := "{ posts { " + strings.Repeat("totalCount ", 7000) + "} }"
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader([]byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(oversizedQuery) + ` + "`" + `}` + "`" + `)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("oversized query response status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GraphQL query is too large") {
		t.Fatalf("oversized query response should name query limit: %s", rec.Body.String())
	}

	var variableDefs strings.Builder
	for i := 0; i < 65; i++ {
		if i > 0 {
			variableDefs.WriteString(",")
		}
		fmt.Fprintf(&variableDefs, "$v%d: String", i)
	}
	variableDefinitionFloodQuery := "query TooMany(" + variableDefs.String() + ") { posts { totalCount } }"
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader([]byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(variableDefinitionFloodQuery) + ` + "`" + `}` + "`" + `)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("variable definition flood response status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GraphQL variable definitions exceed safety limit") {
		t.Fatalf("variable definition flood response should name definition limit: %s", rec.Body.String())
	}

	defaultInputFloodQuery := ` + "`" + `query Defaults($ids: [String] = [` + "`" + ` + strings.TrimSuffix(strings.Repeat(` + "`" + `"x",` + "`" + `, 501), ",") + ` + "`" + `]) { posts { totalCount } }` + "`" + `
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader([]byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(defaultInputFloodQuery) + ` + "`" + `}` + "`" + `)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("variable default flood response status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GraphQL query inputs exceed safety limit") {
		t.Fatalf("variable default flood response should name input limit: %s", rec.Body.String())
	}

	fieldFloodQuery := "{ posts { " + strings.Repeat("totalCount ", 201) + "} }"
	inputFloodQuery := "{ posts(filter: { title: { in: [" + strings.TrimSuffix(strings.Repeat("\"x\",", 501), ",") + "] } }) { totalCount } }"
	deepInputLiteralQuery := "{ posts(filter: { title: { eq: " + strings.Repeat("[", 10) + "\"x\"" + strings.Repeat("]", 10) + " } }) { totalCount } }"
	deepDefaultInputQuery := "query DeepDefault($ids: String = " + strings.Repeat("[", 10) + "\"x\"" + strings.Repeat("]", 10) + ") { posts { totalCount } }"
	variableNodeFlood := map[string]any{"query": "query Good($input: PostFilterInput) { posts(filter: $input) { totalCount } }", "variables": map[string]any{"input": map[string]any{}}}
	for i := 0; i < 256; i++ {
		variableNodeFlood["variables"].(map[string]any)["input"].(map[string]any)[fmt.Sprintf("k%d", i)] = []any{"x", "y"}
	}
	variableNodeFloodBody, err := json.Marshal(variableNodeFlood)
	if err != nil {
		t.Fatal(err)
	}
	extensionNodeFlood := map[string]any{"query": "{ posts { totalCount } }", "extensions": map[string]any{"trace": map[string]any{}}}
	for i := 0; i < 256; i++ {
		extensionNodeFlood["extensions"].(map[string]any)["trace"].(map[string]any)[fmt.Sprintf("k%d", i)] = []any{"x", "y"}
	}
	extensionNodeFloodBody, err := json.Marshal(extensionNodeFlood)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"batched":          []byte(` + "`" + `[{"query":"{ posts { totalCount } }"}]` + "`" + `),
		"duplicate_field":  []byte(` + "`" + `{"query":"{ posts { totalCount } }","query":"{ comments { totalCount } }"}` + "`" + `),
		"unsupported":      []byte(` + "`" + `{"query":"{ posts { totalCount } }","unexpected":true}` + "`" + `),
		"multi_operation":  []byte(` + "`" + `{"query":"query First { posts { totalCount } } query Second { comments { totalCount } }","operationName":"First"}` + "`" + `),
		"invalid_op_name":  []byte(` + "`" + `{"query":"query Good { posts { totalCount } }","operationName":"1 Bad"}` + "`" + `),
		"invalid_id":       []byte(` + "`" + `{"query":"query BadID($id: ID) { posts(filter: { id: { eq: $id } }) { totalCount } }","variables":{"id":"not-a-uuid-secret"}}` + "`" + `),
		"introspection":    []byte(` + "`" + `{"query":"{ __schema { queryType { name } } }"}` + "`" + `),
		"alias_flood":      []byte(` + "`" + `{"query":"{ ` + "`" + ` + strings.Repeat("alias: posts { totalCount } ", 26) + ` + "`" + `}"}` + "`" + `),
		"field_flood":      []byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(fieldFloodQuery) + ` + "`" + `}` + "`" + `),
		"input_flood":      []byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(inputFloodQuery) + ` + "`" + `}` + "`" + `),
		"deep_input":       []byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(deepInputLiteralQuery) + ` + "`" + `}` + "`" + `),
		"deep_default":     []byte(` + "`" + `{"query":` + "`" + ` + mustJSONQuote(deepDefaultInputQuery) + ` + "`" + `}` + "`" + `),
		"operation_flood":  []byte(` + "`" + `{"query":"` + "`" + ` + strings.Repeat("query TooMany { posts { totalCount } } ", 9) + ` + "`" + `"}` + "`" + `),
		"deep_query":       []byte(` + "`" + `{"query":"{ posts { edges { node { id { a { b { c { d { e { f { g } } } } } } } } } } }"}` + "`" + `),
		"bad_variables":    []byte(` + "`" + `{"query":"query Good($id: ID) { post(id: $id) { id } }","variables":["not","object"]}` + "`" + `),
		"deep_variables":   []byte(` + "`" + `{"query":"query Good($v: String) { posts { totalCount } }","variables":{"deep":{"a":{"b":{"c":{"d":{"e":{"f":{"g":{"h":{"i":"too deep"}}}}}}}}}}}` + "`" + `),
		"variable_node_flood": variableNodeFloodBody,
		"huge_number":      []byte(` + "`" + `{"query":"query Good($v: Int) { posts(page: { first: $v }) { totalCount } }","variables":{"v":` + "`" + ` + strings.Repeat("9", 4097) + ` + "`" + `}}` + "`" + `),
		"huge_nested_key":  []byte(` + "`" + `{"query":"query Good($input: PostFilterInput) { posts(filter: $input) { totalCount } }","variables":{"input":{"` + "`" + ` + strings.Repeat("x", 257) + ` + "`" + `":"too large"}}}` + "`" + `),
		"bad_extensions":   []byte(` + "`" + `{"query":"{ posts { totalCount } }","extensions":["not","object"]}` + "`" + `),
		"large_extensions": []byte(` + "`" + `{"query":"{ posts { totalCount } }","extensions":{"trace":"` + "`" + ` + strings.Repeat("x", 4097) + ` + "`" + `"}}` + "`" + `),
		"extension_node_flood": extensionNodeFloodBody,
		"huge_extension_number": []byte(` + "`" + `{"query":"{ posts { totalCount } }","extensions":{"trace":` + "`" + ` + strings.Repeat("9", 4097) + ` + "`" + `}}` + "`" + `),
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || !responseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
				t.Fatalf("%s response status=%d body=%s", name, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "not-a-uuid-secret") {
				t.Fatalf("%s response leaked invalid ID value: %s", name, rec.Body.String())
			}
		})
	}
}

func TestExportedGQLGenTargetHandlerSanitizesResolverDatabaseErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.Post{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	body := []byte(` + "`" + `{"query":"{ posts { totalCount } }"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	graphqlapi.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("closed DB resolver status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !responseHasErrorCode(t, rec.Body.Bytes(), "INTERNAL_SERVER_ERROR") {
		t.Fatalf("closed DB resolver should return INTERNAL_SERVER_ERROR, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sql:") || strings.Contains(rec.Body.String(), "closed") || strings.Contains(rec.Body.String(), "database") {
		t.Fatalf("closed DB resolver response leaked detail: %s", rec.Body.String())
	}

	body = []byte(` + "`" + `{"query":"{ post(id: \"00000000-0000-0000-0000-000000000001\") { id } }"}` + "`" + `)
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	graphqlapi.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("closed DB single resolver status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !responseHasErrorCode(t, rec.Body.Bytes(), "INTERNAL_SERVER_ERROR") {
		t.Fatalf("closed DB single resolver should return INTERNAL_SERVER_ERROR, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "sql:") || strings.Contains(rec.Body.String(), "closed") || strings.Contains(rec.Body.String(), "database") {
		t.Fatalf("closed DB single resolver response leaked detail: %s", rec.Body.String())
	}
}

func responseHasErrorCode(t *testing.T, body []byte, code string) bool {
	t.Helper()
	var resp struct {
		Errors []struct {
			Extensions map[string]any ` + "`" + `json:"extensions"` + "`" + `
		} ` + "`" + `json:"errors"` + "`" + `
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, string(body))
	}
	for _, gqlErr := range resp.Errors {
		if gqlErr.Extensions["code"] == code {
			return true
		}
	}
	return false
}

func mustJSONQuote(value string) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "exported_handler_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedZeroGraphQLRouteTargetBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package commands_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zero-graphql/app/commands"
)

func TestExportedGraphQLRouteUsesHardenedGQLGenTarget(t *testing.T) {
	body := []byte(` + "`" + `{"query":"{ __schema { queryType { name } } }"}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	commands.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "GraphQL introspection is disabled") {
		t.Fatalf("route did not use hardened gqlgen target handler: %s", rec.Body.String())
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "commands", "exported_graphql_route_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedZeroGraphQLAPIErrorBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphqlapi

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"zero-graphql/app/graphqlapi/resolver"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type exportedOwnerOnlyRecord struct {
	UserID string
	Secret string
}

func TestExportedGQLGenTargetErrorPresenterSanitizesUncodedErrors(t *testing.T) {
	presented := graphQLAPIErrorPresenter(context.Background(), errors.New("database password is swordfish"))
	if presented == nil {
		t.Fatal("presenter returned nil")
	}
	if presented.Message != "internal server error" {
		t.Fatalf("presented message = %q", presented.Message)
	}
	if presented.Extensions["code"] != "INTERNAL_SERVER_ERROR" {
		t.Fatalf("presented extensions = %#v", presented.Extensions)
	}
	if strings.Contains(presented.Error(), "swordfish") || strings.Contains(presented.Error(), "password") {
		t.Fatalf("presenter leaked internal detail: %#v", presented)
	}

	coded := graphQLAPIErrorPresenter(context.Background(), graphQLAPICodedError("bad request shape", "BAD_USER_INPUT"))
	if coded == nil || coded.Message != "bad request shape" || coded.Extensions["code"] != "BAD_USER_INPUT" {
		t.Fatalf("coded error was not preserved: %#v", coded)
	}
}

func TestExportedGQLGenTargetRecoverLogsSanitizedMarker(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	err := graphQLAPIRecover(context.Background(), "database password is swordfish")
	gqlErr, ok := err.(*gqlerror.Error)
	if !ok {
		t.Fatalf("recover error type = %T", err)
	}
	if gqlErr.Message != "internal server error" || gqlErr.Extensions["code"] != "INTERNAL_SERVER_ERROR" {
		t.Fatalf("recover error = %v", err)
	}
	if strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "password") {
		t.Fatalf("recover log leaked detail: %s", logs.String())
	}
	if strings.Contains(logs.String(), "goroutine ") || strings.Contains(logs.String(), "graphQLAPIRecover(") {
		t.Fatalf("recover log leaked stack detail: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "graphqlapi panic recovered") {
		t.Fatalf("recover log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedGQLGenTargetOwnerOnlyDirectiveChecksOwner(t *testing.T) {
	called := false
	record := &exportedOwnerOnlyRecord{UserID: "owner-1", Secret: "private"}

	strangerCtx := resolver.WithGraphQLAPIAuthClaims(context.Background(), &resolver.GraphQLAPIAuthClaims{UserID: "stranger", Role: "viewer"})
	got, err := graphQLAPIOwnerOnlyDirective(strangerCtx, record, func(context.Context) (any, error) {
		called = true
		return record.Secret, nil
	})
	if err != nil || got != nil || called {
		t.Fatalf("stranger ownerOnly got=%v err=%v called=%v", got, err, called)
	}

	ownerCtx := resolver.WithGraphQLAPIAuthClaims(context.Background(), &resolver.GraphQLAPIAuthClaims{UserID: "owner-1", Role: "viewer"})
	got, err = graphQLAPIOwnerOnlyDirective(ownerCtx, record, func(context.Context) (any, error) {
		called = true
		return record.Secret, nil
	})
	if err != nil || got != "private" || !called {
		t.Fatalf("owner ownerOnly got=%v err=%v called=%v", got, err, called)
	}

	called = false
	managerCtx := resolver.WithGraphQLAPIAuthClaims(context.Background(), &resolver.GraphQLAPIAuthClaims{UserID: "manager", Role: "viewer", Manages: true, RBACLoaded: true})
	got, err = graphQLAPIOwnerOnlyDirective(managerCtx, record, func(context.Context) (any, error) {
		called = true
		return record.Secret, nil
	})
	if err != nil || got != "private" || !called {
		t.Fatalf("manager ownerOnly got=%v err=%v called=%v", got, err, called)
	}

	got, err = graphQLAPIOwnerOnlyDirective(context.Background(), record, func(context.Context) (any, error) {
		t.Fatal("unauthenticated ownerOnly should not call next")
		return nil, nil
	})
	if err == nil || got != nil {
		t.Fatalf("unauthenticated ownerOnly got=%v err=%v", got, err)
	}

	got, err = graphQLAPIOwnerOnlyDirective(nil, record, func(context.Context) (any, error) {
		t.Fatal("nil-context ownerOnly should not call next")
		return nil, nil
	})
	if err == nil || got != nil {
		t.Fatalf("nil-context ownerOnly got=%v err=%v", got, err)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "exported_error_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportGraphQLSafetyLowersToGQLGenTarget(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "graphql-safety"))
	addDeepGraphQLRelationshipFixture(t, projectDir)
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

	assertFileContains(t, filepath.Join(out, "go.mod"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "schema:")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "- app/graphqlapi/schema.graphqls")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/generated/generated.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: generated")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "filename: app/graphqlapi/model/models_gen.go")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "dir: app/graphqlapi/resolver")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "package: resolver")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), "//go:build tools")
	assertFileContains(t, filepath.Join(out, "tools", "gqlgen.go"), `_ "github.com/99designs/gqlgen"`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func Handler() http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func PlaygroundHandler(endpoint string) http.Handler")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "generated.NewExecutableSchema")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "Auth:        graphQLAPIAuthDirective")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "extension.FixedComplexityLimit")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "Complexity: graphQLAPIComplexityRoot()")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), "root.User.Posts = func(childComplexity int) int")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), "graphQLAPIListComplexity(childComplexity, 10, min(50, maxGraphQLAPIComplexityPageSize))")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), "root.Post.Comments = func(childComplexity int) int")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), `"User.posts":`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "complexity_gen.go"), `"Reaction.flags":`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "generated", "generated.go"), "type ResolverRoot interface")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *Resolver) Query() generated.QueryResolver")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), "func gqlgenPageWindow")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), `graphQLAPIBadInput("page.first and page.last cannot both be set")`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "TotalCount: totalCount")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "TotalCount: len(edges)")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), `panic(fmt.Errorf("not implemented`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "type Query")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "type User")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "posts: [Post!]! @auth")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "comments: [Comment!]! @auth")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *userResolver) Posts")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "if auth := GraphQLAPIAuthFromContext(ctx); auth != nil && !graphQLAPIAuthCanManage(auth)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "q := models.QueryPost().WhereUserID(obj.ID)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "q.WhereUserID(ownerID)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "relationshipLimit := min(50, maxGraphQLAPIPageSize)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "q.Limit(relationshipLimit + 1)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "GraphQL relationship exceeds maximum page size")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *postResolver) Comments")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), "func graphQLAPIAuthCanManage")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), `mime.ParseMediaType(contentType)`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIRequestBodyBytes = 1 << 20")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIRequestEnvelopeFieldBytes = 32")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func validateGraphQLAPIRequestEnvelopeFieldUniqueness")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func validateGraphQLAPIRequestEnvelopeFields")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIQueryBytes = 64 << 10")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "GraphQL query must be a string")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIOperationNameBytes = 256")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func validateGraphQLAPIRequestEnvelope")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIFields = 200")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIVariables = 64")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIOperations = 1")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIInputNodes = 500")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "const maxGraphQLAPIVariableNameBytes = 256")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func validateGraphQLAPIVariables")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "func validateGraphQLAPIExtensions")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "http.MaxBytesReader(w, r.Body, maxGraphQLAPIRequestBodyBytes)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "handler_gen.go"), "extension.FixedComplexityLimit")
	assertNoGoFilesUnder(t, filepath.Join(out, "app", "graphql"))
	assertNoGoFileContains(t, filepath.Join(out, "app", "graphql"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "func (q *UserQuery) WhereID")
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Select([]string{"id", "name"})`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Select([]string{"id", "name", "email"})`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Select([]string{"id", "user_id", "title"})`)
	assertFileContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), "q.db = q.db.Order(OrderClause(column, dir))")
	assertFileNotContains(t, filepath.Join(out, "app", "models", "graphql_query_support.go"), `q.db = q.db.Order(column + " " + dir)`)
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), "q.WhereCreatedAtGTE(value)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), "q.WhereCreatedAtLTE(value)")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "support_gen.go"), `graphQLAPIBadInput("invalid GraphQL timestamp filter")`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `mux.Handle("/graphql", graphqlapi.Handler())`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "routes.API.RegisterRoutes(mux)")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `mux.HandleFunc("/", exportedNotFound)`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "func exportedNotFound")
	assertFileNotContains(t, filepath.Join("..", "..", "go.mod"), "github.com/99designs/gqlgen")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Exported Go GraphQL target backed by gqlgen")
	assertFileNotContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Generated GraphQL package")
	assertCleanExportReport(t, out)
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedGraphQLModelVisibilityBehaviorTest(t, out)
	writeExportedGraphQLAPITargetVisibilityBehaviorTest(t, out)
	writeExportedGraphQLAPIHandlerRBACBehaviorTest(t, out)
	writeExportedGraphQLAPIComplexityBehaviorTest(t, out)
	runExported(t, out, "go", "run", "github.com/99designs/gqlgen", "generate", "--config", "gqlgen.yml")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "generated", "generated.go"), "type ResolverRoot interface")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *Resolver) Query() generated.QueryResolver")
	runExported(t, out, "go", "test", "./...")
}

func addDeepGraphQLRelationshipFixture(t *testing.T, projectDir string) {
	t.Helper()
	migrationPath := filepath.Join(projectDir, "database", "migrations", "2026_06_02_100002_create_reactions_table.go")
	if err := os.WriteFile(migrationPath, []byte(`package migrations

type CreateReactionsTable_2026_06_02_100002 struct {
	Migration
}

func (m *CreateReactionsTable_2026_06_02_100002) Up() {
	m.CreateTable("reactions", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("comment_id").NotNull().ForeignKey("comments", "id")
		t.String("kind", 50).NotNull().Public()
		t.Timestamps()
	})

	m.CreateTable("flags", func(t *Table) {
		t.UUID("id").PrimaryKey().Default("gen_random_uuid()")
		t.UUID("reaction_id").NotNull().ForeignKey("reactions", "id")
		t.String("reason", 255).NotNull().Public()
		t.Timestamps()
	})
}

func (m *CreateReactionsTable_2026_06_02_100002) Down() {
	m.DropTableIfExists("flags")
	m.DropTableIfExists("reactions")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(projectDir, "database", "policies", "graphql", "2026_06_02_100002_expose_deep_relationships.go")
	if err := os.WriteFile(policyPath, []byte(`package graphql

type ExposeDeepRelationships_2026_06_02_100002 struct {
	GraphQLPolicy
}

func (p *ExposeDeepRelationships_2026_06_02_100002) Up() {
	p.AlterExpose("comments", func(e *ExposeBuilder) {
		e.Relationship("reactions", func(r *RelationshipExposure) {
			r.Cost(10)
			r.MaxPageSize(50)
		})
	})
	p.Expose("reactions", func(e *ExposeBuilder) {
		e.List()
		e.Show()
		e.Relationship("flags", func(r *RelationshipExposure) {
			r.Cost(10)
			r.MaxPageSize(50)
		})
	})
	p.Expose("flags", func(e *ExposeBuilder) {
		e.List()
		e.Show()
	})
}

func (p *ExposeDeepRelationships_2026_06_02_100002) Down() {
	p.Unexpose("reactions")
	p.Unexpose("flags")
}
`), 0o644); err != nil {
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

func writeExportedGraphQLAPITargetVisibilityBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package resolver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"graphql-safety/app/graphqlapi/model"
	"graphql-safety/app/models"
)

func TestExportedGQLGenTargetVisibilitySelectsByAuthClaims(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.User{}, &models.Post{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	createdAt := time.Now().UTC().Add(-time.Hour)
	const policyRelationshipPageSize = 50
	user := &models.User{
		ID:           uuid.New(),
		Name:         "Ada",
		Email:        "ada@example.com",
		PasswordHash: "hash",
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}
	if err := models.QueryUser().Create(user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var firstPost *models.Post
	for i := 0; i < policyRelationshipPageSize+5; i++ {
		post := &models.Post{
			ID:        uuid.New(),
			UserID:    user.ID,
			Title:     "Post",
			Body:      "Body",
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
		if err := models.QueryPost().Create(post); err != nil {
			t.Fatalf("create post %d: %v", i, err)
		}
		if firstPost == nil {
			firstPost = post
		}
	}

	queries := &queryResolver{Resolver: &Resolver{}}
	publicUsers, err := queries.Users(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("public users: %v", err)
	}
	if len(publicUsers.Edges) != 1 {
		t.Fatalf("public users edges = %d", len(publicUsers.Edges))
	}
	publicUser := publicUsers.Edges[0].Node
	if publicUser.Name != "Ada" || publicUser.Email != "" || !publicUser.CreatedAt.IsZero() {
		t.Fatalf("public visibility user = %+v", publicUser)
	}

	strangerCtx := WithGraphQLAPIAuthClaims(context.Background(), &GraphQLAPIAuthClaims{UserID: uuid.NewString(), Role: "viewer"})
	strangerUsers, err := queries.Users(strangerCtx, nil, nil, nil)
	if err != nil {
		t.Fatalf("stranger users: %v", err)
	}
	strangerUser := strangerUsers.Edges[0].Node
	if strangerUser.Email != "" || !strangerUser.CreatedAt.IsZero() {
		t.Fatalf("stranger visibility user = %+v", strangerUser)
	}

	ownerCtx := WithGraphQLAPIAuthClaims(context.Background(), &GraphQLAPIAuthClaims{UserID: user.ID.String(), Role: "viewer"})
	ownerUsers, err := queries.Users(ownerCtx, nil, nil, nil)
	if err != nil {
		t.Fatalf("owner users: %v", err)
	}
	ownerUser := ownerUsers.Edges[0].Node
	if ownerUser.Email != "ada@example.com" || !ownerUser.CreatedAt.IsZero() {
		t.Fatalf("owner list visibility user = %+v", ownerUser)
	}

	ownerSingleUser, err := queries.User(ownerCtx, user.ID.String())
	if err != nil {
		t.Fatalf("owner single user: %v", err)
	}
	if ownerSingleUser == nil || ownerSingleUser.Email != "ada@example.com" || !ownerSingleUser.CreatedAt.IsZero() {
		t.Fatalf("owner single visibility user = %+v", ownerSingleUser)
	}

	strangerSingleUser, err := queries.User(strangerCtx, user.ID.String())
	if err != nil {
		t.Fatalf("stranger single user: %v", err)
	}
	if strangerSingleUser == nil || strangerSingleUser.Email != "" || !strangerSingleUser.CreatedAt.IsZero() {
		t.Fatalf("stranger single visibility user = %+v", strangerSingleUser)
	}

	managerCtx := WithGraphQLAPIAuthClaims(context.Background(), &GraphQLAPIAuthClaims{UserID: uuid.NewString(), Role: "viewer", Roles: []string{"tenant_admin"}, Manages: true, RBACLoaded: true})
	managerUsers, err := queries.Users(managerCtx, nil, nil, nil)
	if err != nil {
		t.Fatalf("manager users: %v", err)
	}
	managerUser := managerUsers.Edges[0].Node
	if managerUser.Email != "ada@example.com" || managerUser.CreatedAt.IsZero() {
		t.Fatalf("manager visibility user = %+v", managerUser)
	}

	badTimestamp := "not-a-timestamp"
	if _, err := queries.Users(managerCtx, &model.UserFilter{CreatedAt: &model.DateTimeFilter{Gte: &badTimestamp}}, nil, nil); !isBadInput(err) {
		t.Fatalf("invalid timestamp filter error = %v, want BAD_USER_INPUT", err)
	}
	if offset, err := gqlgenParseCursor("cursor:10"); err != nil || offset != 10 {
		t.Fatalf("valid cursor parsed as %d/%v, want 10/nil", offset, err)
	}
	for _, cursor := range []string{
		"cursor:",
		"cursor:10trailing",
		"cursor:-1",
		"cursor: 10",
		"cursor:" + strings.Repeat("1", maxGraphQLAPICursorBytes),
	} {
		if _, err := gqlgenParseCursor(cursor); err == nil || err.Error() != "invalid cursor" {
			t.Fatalf("cursor %q parse error = %v, want invalid cursor", cursor, err)
		}
	}
	badCursor := "cursor:1trailing"
	if _, err := queries.Users(managerCtx, nil, nil, &model.PageInput{After: &badCursor}); !isBadInput(err) {
		t.Fatalf("bad list cursor error = %v, want BAD_USER_INPUT", err)
	}

	strangerTopPosts, err := queries.Posts(strangerCtx, nil, nil, nil)
	if err != nil {
		t.Fatalf("stranger top-level posts: %v", err)
	}
	if len(strangerTopPosts.Edges) != 0 || strangerTopPosts.TotalCount != 0 {
		t.Fatalf("stranger top-level posts edges/total = %d/%d, want 0/0", len(strangerTopPosts.Edges), strangerTopPosts.TotalCount)
	}

	ownerTopPosts, err := queries.Posts(ownerCtx, nil, nil, nil)
	if err != nil {
		t.Fatalf("owner top-level posts: %v", err)
	}
	if len(ownerTopPosts.Edges) != defaultGraphQLAPIPageSize || ownerTopPosts.TotalCount != policyRelationshipPageSize+5 {
		t.Fatalf("owner top-level posts edges/total = %d/%d, want %d/%d", len(ownerTopPosts.Edges), ownerTopPosts.TotalCount, defaultGraphQLAPIPageSize, policyRelationshipPageSize+5)
	}

	ownerPost, err := queries.Post(ownerCtx, firstPost.ID.String())
	if err != nil || ownerPost == nil || ownerPost.ID != firstPost.ID {
		t.Fatalf("owner top-level post = %+v, %v", ownerPost, err)
	}
	strangerPost, err := queries.Post(strangerCtx, firstPost.ID.String())
	if err != nil || strangerPost != nil {
		t.Fatalf("stranger top-level post = %+v, %v; want nil without error", strangerPost, err)
	}
	managerPost, err := queries.Post(managerCtx, firstPost.ID.String())
	if err != nil || managerPost == nil || managerPost.ID != firstPost.ID {
		t.Fatalf("manager top-level post = %+v, %v", managerPost, err)
	}

	userFields := &userResolver{Resolver: &Resolver{}}
	if _, err := userFields.Posts(context.Background(), user); !isUnauthenticated(err) {
		t.Fatalf("unauthenticated relationship error = %v, want UNAUTHENTICATED", err)
	}

	strangerPosts, err := userFields.Posts(strangerCtx, user)
	if err != nil {
		t.Fatalf("stranger posts: %v", err)
	}
	if len(strangerPosts) != 0 {
		t.Fatalf("stranger relationship posts = %d, want 0", len(strangerPosts))
	}

	ownerPosts, err := userFields.Posts(ownerCtx, user)
	if !isBadInput(err) || ownerPosts != nil {
		t.Fatalf("owner overflowing relationship posts = %d/%v, want BAD_USER_INPUT and nil", len(ownerPosts), err)
	}

	posts, err := userFields.Posts(managerCtx, user)
	if !isBadInput(err) || posts != nil {
		t.Fatalf("manager overflowing relationship posts = %d/%v, want BAD_USER_INPUT and nil", len(posts), err)
	}

	if err := models.DB.Where("1 = 1").Delete(&models.Post{}).Error; err != nil {
		t.Fatalf("delete overflowing posts: %v", err)
	}
	var firstAllowedPost *models.Post
	for i := 0; i < policyRelationshipPageSize; i++ {
		post := &models.Post{
			ID:        uuid.New(),
			UserID:    user.ID,
			Title:     "Allowed",
			Body:      "Body",
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
		if err := models.QueryPost().Create(post); err != nil {
			t.Fatalf("create allowed post %d: %v", i, err)
		}
		if firstAllowedPost == nil {
			firstAllowedPost = post
		}
	}
	posts, err = userFields.Posts(managerCtx, user)
	if err != nil {
		t.Fatalf("bounded user posts: %v", err)
	}
	if len(posts) != policyRelationshipPageSize {
		t.Fatalf("bounded relationship posts = %d, want %d", len(posts), policyRelationshipPageSize)
	}
	if posts[0].UserID != user.ID || posts[0].ID != firstAllowedPost.ID {
		t.Fatalf("relationship first post = %+v, want user %s post %s", posts[0], user.ID, firstAllowedPost.ID)
	}
}

func isBadInput(err error) bool {
	if err == nil {
		return false
	}
	gqlErr, ok := err.(*gqlerror.Error)
	if !ok {
		return false
	}
	return gqlErr.Extensions["code"] == "BAD_USER_INPUT"
}

func isUnauthenticated(err error) bool {
	if err == nil {
		return false
	}
	gqlErr, ok := err.(*gqlerror.Error)
	if !ok {
		return false
	}
	return gqlErr.Extensions["code"] == "UNAUTHENTICATED"
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "resolver", "exported_visibility_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedGraphQLAPIHandlerRBACBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphqlapi

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"graphql-safety/app/http/auth"
	"graphql-safety/app/http/auth/jwt"
	"graphql-safety/app/models"
)

func TestExportedGQLGenTargetAuthLoadsDatabaseRoles(t *testing.T) {
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
	userID := uuid.NewString()
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
	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{
		Subject:   userID,
		Role:      "viewer",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	claims, err := extractGraphQLAPIAuth(req)
	if err != nil {
		t.Fatalf("extract auth: %v", err)
	}
	if claims == nil || claims.UserID != userID || !claims.Manages || !claims.RBACLoaded {
		t.Fatalf("claims = %+v", claims)
	}
	if !graphQLAPIHasRole(claims, "tenant_admin") {
		t.Fatalf("database role not recognized: %+v", claims)
	}
	if graphQLAPIHasRole(claims, "viewer") {
		t.Fatalf("token fallback role should not grant after RBAC load: %+v", claims)
	}
}

func TestExportedGQLGenTargetAuthRBACFallbackOnlyWhenSchemaMissing(t *testing.T) {
	userID := uuid.NewString()
	tokenRole := "admin"

	missingDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := missingDB.Exec(` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `).Error; err != nil {
		t.Fatal(err)
	}
	models.SetDB(missingDB)
	missingSQLDB, err := missingDB.DB()
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
	}, missingSQLDB)
	missingToken, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{
		Subject:   userID,
		Role:      tokenRole,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign missing-schema jwt: %v", err)
	}
	missingReq, err := http.NewRequest(http.MethodPost, "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}
	missingReq.Header.Set("Authorization", "Bearer "+missingToken)
	missingClaims, err := extractGraphQLAPIAuth(missingReq)
	if err != nil {
		t.Fatalf("missing schema auth should use token fallback: %v", err)
	}
	if missingClaims == nil || missingClaims.RBACLoaded || !graphQLAPIHasRole(missingClaims, tokenRole) {
		t.Fatalf("missing schema claims = %+v, want token fallback role", missingClaims)
	}

	models.SetDB(nil)
	nilDBReq, err := http.NewRequest(http.MethodPost, "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}
	nilDBReq.Header.Set("Authorization", "Bearer "+missingToken)
	if claims, err := extractGraphQLAPIAuth(nilDBReq); err == nil {
		t.Fatalf("nil GraphQL RBAC DB should fail closed, got claims %+v", claims)
	} else if err.Error() != "graphql rbac database unavailable" || strings.Contains(err.Error(), "SELECT") || strings.Contains(err.Error(), "jwt_tokens") {
		t.Fatalf("nil GraphQL RBAC DB error = %v, want sanitized unavailable error", err)
	}
	models.SetDB(missingDB)

	if exists, err := graphQLAPIRBACTableExists("roles; DROP TABLE role_user"); err == nil || exists {
		t.Fatalf("unsafe RBAC table probe should be rejected, exists=%v err=%v", exists, err)
	} else if err.Error() != "graphql rbac schema check rejected" || strings.Contains(err.Error(), "DROP") {
		t.Fatalf("unsafe RBAC table probe error = %v, want sanitized rejection", err)
	}

	partialDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := partialDB.Exec(` + "`" + `CREATE TABLE jwt_tokens (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, expires_at DATETIME NOT NULL, revoked_at DATETIME, created_at DATETIME NOT NULL)` + "`" + `).Error; err != nil {
		t.Fatal(err)
	}
	if err := partialDB.Exec(` + "`" + `CREATE TABLE roles (id TEXT PRIMARY KEY, slug TEXT NOT NULL, manages BOOLEAN NOT NULL)` + "`" + `).Error; err != nil {
		t.Fatal(err)
	}
	models.SetDB(partialDB)
	partialSQLDB, err := partialDB.DB()
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
	}, partialSQLDB)
	partialToken, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{
		Subject:   userID,
		Role:      tokenRole,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign partial-schema jwt: %v", err)
	}
	partialReq, err := http.NewRequest(http.MethodPost, "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}
	partialReq.Header.Set("Authorization", "Bearer "+partialToken)
	if claims, err := extractGraphQLAPIAuth(partialReq); err == nil {
		t.Fatalf("partial RBAC schema should fail closed, got claims %+v", claims)
	} else if err.Error() != "graphql rbac schema incomplete" || strings.Contains(err.Error(), "role_user") || strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("partial RBAC schema error = %v, want sanitized incomplete schema error", err)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "exported_rbac_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedGraphQLAPIComplexityBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphqlapi

import (
	"strings"
	"testing"

	"graphql-safety/app/graphqlapi/generated"
	"graphql-safety/app/graphqlapi/model"
	"graphql-safety/app/graphqlapi/resolver"
)

func TestExportedGQLGenTargetRelationshipComplexityUsesPolicyBudget(t *testing.T) {
	root := graphQLAPIComplexityRoot()

	if root.User.Posts == nil {
		t.Fatal("User.posts complexity hook was not generated")
	}
	if root.Post.Comments == nil {
		t.Fatal("Post.comments complexity hook was not generated")
	}

	if got, want := root.User.Posts(1), 550; got != want {
		t.Fatalf("User.posts complexity = %d, want %d", got, want)
	}
	if got, want := root.Post.Comments(1), 550; got != want {
		t.Fatalf("Post.comments complexity = %d, want %d", got, want)
	}
	if got := root.User.Posts(91); got <= maxGraphQLAPIComplexity {
		t.Fatalf("User.posts high child complexity = %d, want above max %d", got, maxGraphQLAPIComplexity)
	}
}

func TestExportedGQLGenTargetTopLevelComplexityValidatesPageInput(t *testing.T) {
	root := graphQLAPIComplexityRoot()
	if root.Query.Users == nil {
		t.Fatal("Query.users complexity hook was not generated")
	}

	first := 10
	if got, want := root.Query.Users(1, nil, nil, &model.PageInput{First: &first}), 20; got != want {
		t.Fatalf("Query.users complexity = %d, want %d", got, want)
	}

	first = 101
	if got := root.Query.Users(0, nil, nil, &model.PageInput{First: &first}); got <= maxGraphQLAPIComplexity {
		t.Fatalf("oversized Query.users page complexity = %d, want above max %d", got, maxGraphQLAPIComplexity)
	}
}

func TestExportedGQLGenTargetRejectsRelationshipDepthBeforeExecution(t *testing.T) {
	execSchema := generated.NewExecutableSchema(generated.Config{
		Resolvers:  &resolver.Resolver{},
		Complexity: graphQLAPIComplexityRoot(),
		Directives: generated.DirectiveRoot{
			Auth:        graphQLAPIAuthDirective,
			Public:      graphQLAPIPublicDirective,
			OwnerOnly:   graphQLAPIOwnerOnlyDirective,
			RequireRole: graphQLAPIRequireRoleDirective,
		},
	})
	query := ` + "`" + `{
		users {
			edges {
				node {
					posts {
						comments {
							reactions {
								flags {
									id
								}
							}
						}
					}
				}
			}
		}
	}` + "`" + `
	err := validateGraphQLAPIQueryShape(execSchema.Schema(), query)
	if err == nil {
		t.Fatal("relationship depth query passed preflight budget")
	}
	if !strings.Contains(err.Error(), "GraphQL relationship depth exceeds safety limit") {
		t.Fatalf("relationship depth error = %v", err)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "exported_complexity_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExportMonorepoCompiles(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "monorepo"))
	workerRoutePath := filepath.Join(projectDir, "services", "worker", "routes", "web.go")
	workerRoutesData, err := os.ReadFile(workerRoutePath)
	if err != nil {
		t.Fatal(err)
	}
	workerRoutesSource := strings.Replace(string(workerRoutesData), "var API = pickle.Routes", "var Worker = pickle.Routes", 1)
	if workerRoutesSource == string(workerRoutesData) {
		t.Fatal("worker route fixture did not contain API route var")
	}
	if err := os.WriteFile(workerRoutePath, []byte(workerRoutesSource), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err = Export(Options{
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
	assertFileNotContains(t, filepath.Join(out, "internal", "httpx", "httpx.go"), "pickle:")
	assertFileNotContains(t, filepath.Join(out, "internal", "httpx", "httpx.go"), "pickle export:")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "apiRoutes")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "workerRoutes")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "apiRoutes.API.RegisterRoutes(mux)")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "workerRoutes.Worker.RegisterRoutes(workerMux)")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `http.StripPrefix("/worker", workerMux)`)
	assertFileNotContains(t, filepath.Join(out, "cmd", "server", "main.go"), "workerRoutes.API")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), `mux.HandleFunc("/", exportedNotFound)`)
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "func exportedNotFound")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "ReadHeaderTimeout: 10 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "ReadTimeout:       30 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "WriteTimeout:      60 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "IdleTimeout:       120 * time.Second")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "MaxHeaderBytes:    1 << 20")
	assertCleanExportReport(t, out)
	assertStandaloneNoPickleRuntime(t, out)
	assertNoGoFileContains(t, out, "QueryOrder")
	writeExportedMonorepoServerBehaviorTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedMonorepoServerBehaviorTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"monorepo/app/models"
	apiRoutes "monorepo/services/api/routes"
	workerRoutes "monorepo/services/worker/routes"
)

func TestExportedMultiServiceServerMountsServiceLocalRoutes(t *testing.T) {
	t.Setenv("RATE_LIMIT", "false")
	t.Setenv("APP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	user := &models.User{
		ID:           uuid.New(),
		Name:         "Ada",
		Email:        "ada@example.com",
		PasswordHash: "hash",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed exported monorepo user: %v", err)
	}

	mux := http.NewServeMux()
	apiRoutes.API.RegisterRoutes(mux)
	workerMux := http.NewServeMux()
	workerRoutes.Worker.RegisterRoutes(workerMux)
	mux.Handle("/worker/", http.StripPrefix("/worker", workerMux))
	mux.HandleFunc("/", exportedNotFound)

	usersRec := httptest.NewRecorder()
	mux.ServeHTTP(usersRec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if usersRec.Code != http.StatusOK {
		t.Fatalf("api users status = %d body=%s", usersRec.Code, usersRec.Body.String())
	}
	if !strings.Contains(usersRec.Body.String(), "\"name\":\"Ada\"") {
		t.Fatalf("api users response missing seeded user: %s", usersRec.Body.String())
	}
	for _, leak := range []string{"password", "password_hash", "hash"} {
		if strings.Contains(usersRec.Body.String(), leak) {
			t.Fatalf("api users response leaked %q: %s", leak, usersRec.Body.String())
		}
	}

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

	missingRec := httptest.NewRecorder()
	mux.ServeHTTP(missingRec, httptest.NewRequest(http.MethodGet, "/pickle/health", nil))
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing route status = %d body=%s", missingRec.Code, missingRec.Body.String())
	}
	if got := missingRec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("missing route Content-Type = %q, want application/json", got)
	}
	if got := missingRec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("missing route X-Content-Type-Options = %q, want nosniff", got)
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

type passwordSwordfishJob struct{}

func (*passwordSwordfishJob) Handle() error { return errExportedFlaky }

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
	for _, leak := range []string{"swordfish", "password", "exportedFlakyJob"} {
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("scheduler retry log leaked %q: %s", leak, logs.String())
		}
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
	for _, leak := range []string{"swordfish", "password=", "expected exactly", "exportedFlakyJob"} {
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("invalid schedule log leaked %q: %s", leak, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "schedule rejected") {
		t.Fatalf("invalid schedule log missing sanitized marker: %s", logs.String())
	}
}

func TestExportedSchedulerNilInputsDoNotPanic(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	scheduler := Cron(nil)
	if scheduler == nil {
		t.Fatal("Cron(nil) returned nil scheduler")
	}
	if entries := scheduler.Entries(); len(entries) != 0 {
		t.Fatalf("Cron(nil) entries = %d, want 0", len(entries))
	}
	scheduler.Start(nil)

	var nilScheduler *Scheduler
	if entries := nilScheduler.Entries(); entries != nil {
		t.Fatalf("nil scheduler entries = %#v, want nil", entries)
	}
	entry := nilScheduler.Job("*/5 * * * *", &exportedFlakyJob{})
	if entry == nil || entry.Schedule != "*/5 * * * *" {
		t.Fatalf("nil scheduler Job returned %#v", entry)
	}
	nilScheduler.Start(context.Background())

	if got := ((*JobEntry)(nil)).MaxRetries(-1).RetryDelay(-time.Second).Timeout(-time.Second).SkipIfRunning().AllowOverlap(); got != nil {
		t.Fatalf("nil entry chain returned %#v, want nil", got)
	}
	runJob(nil)
	runJob(&JobEntry{})
	err := safeHandleJob(nil)
	if err == nil || err.Error() != "job failed" {
		t.Fatalf("safeHandleJob(nil) = %v, want sanitized job failed", err)
	}
	if strings.Contains(logs.String(), "panic") || strings.Contains(logs.String(), "swordfish") || strings.Contains(logs.String(), "password") {
		t.Fatalf("nil input handling leaked detail: %s", logs.String())
	}
}

func TestExportedSchedulerLogsDoNotLeakJobTypeNames(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLogOutput)

	runJob(&JobEntry{Job: &passwordSwordfishJob{}})
	for _, leak := range []string{"passwordSwordfishJob", "password", "Swordfish", "swordfish"} {
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("scheduler log leaked %q: %s", leak, logs.String())
		}
	}
	if !strings.Contains(logs.String(), "job failed") {
		t.Fatalf("scheduler log missing sanitized failure marker: %s", logs.String())
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

	serverTest := `package main

import (
	"bytes"
	"database/sql"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type exportedCronBinaryServer struct {
	cmd          *exec.Cmd
	output       *bytes.Buffer
	done         chan error
	doneConsumed bool
}

func TestExportedCronServerBinaryStartsSchedulerAndServesRoutes(t *testing.T) {
	port := freeExportedCronPort(t)
	dbPath := filepath.Join(t.TempDir(), "cron-server.sqlite")
	migrate := exec.Command("go", "run", ".", "migrate")
	migrate.Env = exportedCronBinaryEnv(port, dbPath)
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("go run . migrate failed: %v\n%s", err, output)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got := countExportedCronRows(t, db, "migrations"); got == 0 {
		t.Fatal("binary migrate did not record app migrations")
	}

	server := startExportedCronBinaryServer(t, port, dbPath)
	defer server.cleanup(t)

	resp, body := waitForExportedCronHTTP(t, server, "http://127.0.0.1:"+port+"/api/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("server status = %d body=%s output=%s", resp.StatusCode, body, server.output.String())
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("server Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(string(body), "Welcome to Pickle!") {
		t.Fatalf("server body = %s, want welcome message", body)
	}

	miss, missBody := waitForExportedCronHTTP(t, server, "http://127.0.0.1:"+port+"/missing-password=swordfish")
	if miss.StatusCode != http.StatusNotFound {
		t.Fatalf("missing route status = %d body=%s output=%s", miss.StatusCode, missBody, server.output.String())
	}
	if got := miss.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("missing route X-Content-Type-Options = %q, want nosniff", got)
	}
	if strings.Contains(string(missBody), "swordfish") || strings.Contains(string(missBody), "missing-password") {
		t.Fatalf("missing route leaked request detail: %s", missBody)
	}

	for _, leak := range []string{"CleanupJob", "SendDigestJob", "secret", "swordfish", "github.com/shortontech/pickle"} {
		if strings.Contains(server.output.String(), leak) {
			t.Fatalf("cron server output leaked %q: %s", leak, server.output.String())
		}
	}
}

func waitForExportedCronHTTP(t *testing.T, server *exportedCronBinaryServer, url string) (*http.Response, []byte) {
	t.Helper()
	var resp *http.Response
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-server.done:
			server.doneConsumed = true
			t.Fatalf("exported cron server binary exited before serving: %v\n%s", waitErr, server.output.String())
		default:
		}
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("exported cron server binary did not serve %s: %v\n%s", url, err, server.output.String())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

func startExportedCronBinaryServer(t *testing.T, port, dbPath string) *exportedCronBinaryServer {
	t.Helper()
	cmd := exec.Command("go", "run", ".")
	cmd.Env = exportedCronBinaryEnv(port, dbPath)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start exported cron server binary: %v", err)
	}
	server := &exportedCronBinaryServer{
		cmd:    cmd,
		output: &output,
		done:   make(chan error, 1),
	}
	go func() { server.done <- cmd.Wait() }()
	return server
}

func (s *exportedCronBinaryServer) cleanup(t *testing.T) {
	t.Helper()
	if s == nil || s.doneConsumed {
		return
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("exported cron server binary did not exit after kill; output=%s", s.output.String())
	}
}

func exportedCronBinaryEnv(port, dbPath string) []string {
	return append(os.Environ(),
		"APP_PORT="+port,
		"DB_CONNECTION=sqlite",
		"DB_DATABASE="+dbPath,
		"JWT_SECRET=0123456789abcdef0123456789abcdef",
		"APP_ENCRYPTION_KEY=12345678901234567890123456789012",
	)
}

func freeExportedCronPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func countExportedCronRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
`
	if err := os.WriteFile(filepath.Join(out, "cmd", "server", "exported_cron_server_test.go"), []byte(serverTest), 0o644); err != nil {
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
	assertStandaloneNoPickleRuntime(t, out)
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

func TestExportFailsUnsupportedORMWithBoundaryRule(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		ORM:          "sqlc",
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported ORM")
	}
	assertContainsAll(t, err.Error(),
		"[orm_export_unsupported]",
		`unsupported orm "sqlc"`,
	)
	assertPathMissing(t, out)
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

func TestExportFailsUnsupportedActionSignatureWithBoundaryRule(t *testing.T) {
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

func (a SuspendAction) Suspend(ctx *pickle.Context, user *models.User) string {
	return "not an error"
}
`
	gate := `package user

import "github.com/google/uuid"

func CanSuspend(ctx *Context, user *User) *uuid.UUID {
	id := uuid.New()
	return &id
}
`
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend_gate.go"), []byte(gate), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported action signature")
	}
	assertContainsAll(t, err.Error(),
		"database/actions/user/suspend.go:",
		"[action_export_unsupported_signature]",
		"action Suspend has a signature that cannot be lowered safely",
	)
}

func TestExportFailsUnsupportedActionParamsWithBoundaryRule(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	actionsDir := filepath.Join(projectDir, "database", "actions", "user")
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	action := `package user

type SuspendAction struct{}

func (a SuspendAction) Suspend(ctx string, user int) error {
	return nil
}
`
	gate := `package user

import "github.com/google/uuid"

func CanSuspend(ctx *Context, user *User) *uuid.UUID {
	id := uuid.New()
	return &id
}
`
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend_gate.go"), []byte(gate), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported action params")
	}
	assertContainsAll(t, err.Error(),
		"database/actions/user/suspend.go:",
		"[action_export_unsupported_signature]",
		"action Suspend has a signature that cannot be lowered safely",
	)
}

func TestExportFailsUnsupportedGateSignatureWithBoundaryRule(t *testing.T) {
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
	gate := `package user

func CanSuspend(ctx *Context, user *User) bool {
	return true
}
`
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend_gate.go"), []byte(gate), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported gate signature")
	}
	assertContainsAll(t, err.Error(),
		"database/actions/user/suspend_gate.go:",
		"[gate_export_unsupported_signature]",
		"gate CanSuspend has a signature that cannot be lowered safely",
	)
}

func TestExportFailsUnsupportedGateParamsWithBoundaryRule(t *testing.T) {
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
	gate := `package user

import "github.com/google/uuid"

func CanSuspend(ctx string, user int) *uuid.UUID {
	id := uuid.New()
	return &id
}
`
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend_gate.go"), []byte(gate), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported gate params")
	}
	assertContainsAll(t, err.Error(),
		"database/actions/user/suspend_gate.go:",
		"[gate_export_unsupported_signature]",
		"gate CanSuspend has a signature that cannot be lowered safely",
	)
}

func TestExportFailsUnsupportedActionQueryWithBoundaryRule(t *testing.T) {
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

type SuspendAction struct {
	SQL string
}

func (a SuspendAction) Suspend(ctx *pickle.Context, user *models.User) error {
	_, err := models.QueryUser().Raw(a.SQL).All()
	return err
}
`
	gate := `package user

import "github.com/google/uuid"

func CanSuspend(ctx *Context, user *User) *uuid.UUID {
	id := uuid.New()
	return &id
}
`
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "suspend_gate.go"), []byte(gate), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported action query")
	}
	assertContainsAll(t, err.Error(),
		"database/actions/user/suspend.go:",
		"[action_export_unsupported_query]",
		"unsupported query method Raw",
	)
	assertPathMissing(t, filepath.Join(out, "app", "models", "user_suspend.go"))
}

func TestExportFailsUnsupportedQueryWithBoundaryRule(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "zero-graphql"))
	servicesDir := filepath.Join(projectDir, "app", "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package services

import models "github.com/shortontech/pickle/testdata/zero-graphql/app/models"

func UnsafeRawUsers(sql string) ([]models.User, error) {
	return models.QueryUser().Raw(sql).All()
}
`
	if err := os.WriteFile(filepath.Join(servicesDir, "unsafe_raw_users.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "exported")
	_, err := Export(Options{
		ProjectDir:   projectDir,
		OutDir:       out,
		Force:        true,
		PicklePkgDir: filepath.Join("..", "..", "pkg"),
	})
	if err == nil {
		t.Fatal("Export succeeded for unsupported raw query")
	}
	got := err.Error()
	assertContainsAll(t, got,
		"unsafe_raw_users.go:",
		"[query_export_unsupported]",
		"unsupported query method Raw",
	)
	assertPathMissing(t, filepath.Join(out, "app", "services", "unsafe_raw_users.go"))
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
	if err == nil {
		t.Fatal("generateSQLMigrations succeeded for unsupported migration")
	}
	assertContainsAll(t, err.Error(),
		filepath.Join("database", "migrations", "2026_02_21_100000_add_email_to_users_table.go"),
		"[migration_export_unsupported]",
		"add-column/index migrations are not lowered yet",
	)
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

func TestGenerateSQLMigrationsReportsUnsupportedCapturedOperationBoundary(t *testing.T) {
	ex := &exporter{migrations: []generator.MigrationOps{
		{
			Name: "RewriteUsers_2026_06_06_120000",
			Up: []generator.MigrationOperation{
				{Type: "rewrite_table", Table: "users"},
			},
		},
	}}
	_, err := ex.generateSQLMigrations(nil, nil)
	if err == nil {
		t.Fatal("generateSQLMigrations succeeded for unsupported captured operation")
	}
	assertContainsAll(t, err.Error(),
		"database/migrations:",
		"[migration_export_unsupported]",
		"unsupported migration export for RewriteUsers_2026_06_06_120000",
		"rewrite_table migrations are not lowered yet",
	)
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
		`AddError(graphQLModelBadInput("encrypted column email does not support Like filters"))`,
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
		`AddError(fmt.Errorf("encrypted column email does not support Like filters"))`,
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
	assertStandaloneNoPickleRuntime(t, out)

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

func TestExportReportsRawSQLMigrationsAsManualReviewEndToEnd(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "cron-test"))
	migrationPath := filepath.Join(projectDir, "database", "migrations", "2026_06_05_120000_raw_manual_review.go")
	if err := os.WriteFile(migrationPath, []byte(`package migrations

type RawManualReview_2026_06_05_120000 struct {
	Migration
}

func (m *RawManualReview_2026_06_05_120000) Up() {
	m.RawSQL(`+"`"+`CREATE TABLE raw_manual_items (id TEXT PRIMARY KEY, note TEXT NOT NULL);`+"`"+`)
}

func (m *RawManualReview_2026_06_05_120000) Down() {
	m.RawSQL(`+"`"+`DROP TABLE raw_manual_items;`+"`"+`)
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if !hasFinding(res.Findings, "raw_sql_migration") {
		t.Fatalf("expected raw_sql_migration finding, got %+v", res.Findings)
	}

	reportPath := filepath.Join(out, "EXPORT_REPORT.md")
	assertFileContains(t, reportPath, "## Unsupported\n\nNo unsupported export findings.")
	assertFileContains(t, reportPath, "## Manual Review")
	assertFileContains(t, reportPath, "`database/migrations` `raw_sql_migration` - migration RawManualReview_2026_06_05_120000 contains raw SQL; exported statements need driver-specific review")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260605120000_raw_manual_review.up.sql"), "CREATE TABLE raw_manual_items")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260605120000_raw_manual_review.down.sql"), "DROP TABLE raw_manual_items")
	assertStandaloneNoPickleRuntime(t, out)

	behaviorTest := `package migrations

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedRawSQLMigrationRunsAndRollsBack(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(db, "sqlite")
	if err := runner.Migrate(Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !tableExists(t, db, "raw_manual_items") {
		t.Fatal("raw_manual_items table missing after migrate")
	}
	if err := runner.Rollback(Registry); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if tableExists(t, db, "raw_manual_items") {
		t.Fatal("raw_manual_items table still exists after rollback")
	}
}

func tableExists(t *testing.T, db *gorm.DB, table string) bool {
	t.Helper()
	var name string
	err := db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name).Error
	if err != nil {
		t.Fatal(err)
	}
	return name == table
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "migrations", "exported_raw_sql_behavior_test.go"), []byte(behaviorTest), 0o644); err != nil {
		t.Fatal(err)
	}
	runExported(t, out, "go", "test", "./database/migrations")
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
			Findings: []Finding{
				{
					File:    filepath.Join("database", "policies", "graphql"),
					Rule:    "graphql_action_export_unsupported",
					Message: "GraphQL controller action approveTransfer is not lowered by the exported Go GraphQL target backed by gqlgen",
				},
				{
					File:    filepath.Join("database", "migrations", "2026_02_21_100000_add_email_to_users_table.go"),
					Rule:    "migration_export_unsupported",
					Message: "unsupported migration export for 2026_02_21_100000_add_email_to_users_table.go: add-column/index migrations are not lowered yet",
				},
			},
		},
	}
	if err := ex.writeReport("gorm"); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	assertFileContains(t, reportPath, "## Unsupported")
	assertFileContains(t, reportPath, "`database/policies/graphql` `graphql_action_export_unsupported` - GraphQL controller action approveTransfer is not lowered by the exported Go GraphQL target backed by gqlgen")
	assertFileContains(t, reportPath, "`database/migrations/2026_02_21_100000_add_email_to_users_table.go` `migration_export_unsupported` - unsupported migration export for 2026_02_21_100000_add_email_to_users_table.go: add-column/index migrations are not lowered yet")
	assertFileNotContains(t, reportPath, "No unsupported export findings.")
	assertFileNotContains(t, reportPath, "## Manual Review")
}

func TestExportReportsGraphQLControllerActionsEndToEnd(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	policyPath := filepath.Join(projectDir, "database", "policies", "graphql", "2026_06_05_100000_actions.go")
	if err := os.WriteFile(policyPath, []byte(`package graphql

type ActionAPI_2026_06_05_100000 struct {
	GraphQLPolicy
}

func (p *ActionAPI_2026_06_05_100000) Up() {
	p.ControllerAction("approveTransfer", nil)
}

func (p *ActionAPI_2026_06_05_100000) Down() {
	p.RemoveAction("approveTransfer")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if !hasFinding(res.Findings, "graphql_action_export_unsupported") {
		t.Fatalf("expected graphql_action_export_unsupported finding, got %+v", res.Findings)
	}
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Unsupported")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "`database/policies/graphql` `graphql_action_export_unsupported` - GraphQL controller action approveTransfer is not lowered by the exported Go GraphQL target backed by gqlgen")
	assertFileNotContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "No unsupported export findings.")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "approveTransfer")
	assertFileNotContains(t, filepath.Join(out, "database", "policies", "support.go"), "approveTransfer")
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedUnsupportedGraphQLActionPolicyStateTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportReportsUnsupportedGraphQLControllerActionSignature(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	controllerPath := filepath.Join(projectDir, "app", "http", "controllers", "transfer_controller.go")
	if err := os.WriteFile(controllerPath, []byte(`package controllers

import (
	"net/http"

	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type TransferController struct{}

func (c TransferController) Approve(ctx *pickle.Context, force bool) pickle.Response {
	return ctx.JSON(http.StatusAccepted, map[string]any{"ok": force})
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(projectDir, "database", "policies", "graphql", "2026_06_05_100000_actions.go")
	if err := os.WriteFile(policyPath, []byte(`package graphql

import "github.com/shortontech/pickle/testdata/basic-crud/app/http/controllers"

type ActionAPI_2026_06_05_100000 struct {
	GraphQLPolicy
}

func (p *ActionAPI_2026_06_05_100000) Up() {
	p.ControllerAction("approveTransfer", controllers.TransferController{}.Approve)
}

func (p *ActionAPI_2026_06_05_100000) Down() {
	p.RemoveAction("approveTransfer")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if !hasFinding(res.Findings, "graphql_action_export_unsupported") {
		t.Fatalf("expected graphql_action_export_unsupported finding, got %+v", res.Findings)
	}
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "`database/policies/graphql` `graphql_action_export_unsupported` - GraphQL controller action approveTransfer (controllers.TransferController{}.Approve) is not lowered by the exported Go GraphQL target backed by gqlgen")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "approveTransfer")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "ApproveTransfer")
	assertFileNotContains(t, filepath.Join(out, "database", "policies", "support.go"), "approveTransfer")
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedUnsupportedGraphQLActionPolicyStateTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportLowersSupportedGraphQLControllerActionsEndToEnd(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	controllerPath := filepath.Join(projectDir, "app", "http", "controllers", "transfer_controller.go")
	if err := os.WriteFile(controllerPath, []byte(`package controllers

import (
	"net/http"

	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type TransferController struct{}

func (c TransferController) Approve(ctx *pickle.Context) pickle.Response {
	return ctx.JSON(http.StatusAccepted, map[string]any{
		"ok": true,
		"userId": ctx.Auth().UserID,
		"role": ctx.Role(),
	})
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(projectDir, "database", "policies", "graphql", "2026_06_05_100000_actions.go")
	if err := os.WriteFile(policyPath, []byte(`package graphql

import "github.com/shortontech/pickle/testdata/basic-crud/app/http/controllers"

type ActionAPI_2026_06_05_100000 struct {
	GraphQLPolicy
}

func (p *ActionAPI_2026_06_05_100000) Up() {
	p.ControllerAction("approveTransfer", controllers.TransferController{}.Approve)
}

func (p *ActionAPI_2026_06_05_100000) Down() {
	p.RemoveAction("approveTransfer")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if hasFinding(res.Findings, "graphql_action_export_unsupported") {
		t.Fatalf("did not expect graphql_action_export_unsupported finding, got %+v", res.Findings)
	}
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Unsupported\n\nNo unsupported export findings.")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "scalar JSON")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "approveTransfer(input: JSON!): JSON! @auth")
	assertFileContains(t, filepath.Join(out, "gqlgen.yml"), "map[string]interface{}")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *mutationResolver) ApproveTransfer")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "controllers.TransferController{}.Approve")
	assertFileContains(t, filepath.Join(out, "database", "policies", "support.go"), `{Name: "approveTransfer"}`)
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedSupportedGraphQLActionPolicyStateTest(t, out)
	writeExportedSupportedGraphQLActionResolverTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func TestExportLowersGraphQLControllerActionsWithoutModelExposure(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
	if err := os.Remove(filepath.Join(projectDir, "database", "policies", "graphql", "2026_03_25_100000_expose_users.go")); err != nil {
		t.Fatal(err)
	}
	controllerPath := filepath.Join(projectDir, "app", "http", "controllers", "transfer_controller.go")
	if err := os.WriteFile(controllerPath, []byte(`package controllers

import (
	"net/http"

	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
)

type TransferController struct{}

func (c TransferController) Approve(ctx *pickle.Context) pickle.Response {
	return ctx.JSON(http.StatusAccepted, map[string]any{
		"ok": true,
		"userId": ctx.Auth().UserID,
	})
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(projectDir, "database", "policies", "graphql", "2026_06_05_100000_actions.go")
	if err := os.WriteFile(policyPath, []byte(`package graphql

import "github.com/shortontech/pickle/testdata/basic-crud/app/http/controllers"

type ActionAPI_2026_06_05_100000 struct {
	GraphQLPolicy
}

func (p *ActionAPI_2026_06_05_100000) Up() {
	p.ControllerAction("approveTransfer", controllers.TransferController{}.Approve)
}

func (p *ActionAPI_2026_06_05_100000) Down() {
	p.RemoveAction("approveTransfer")
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

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
	if hasFinding(res.Findings, "graphql_action_export_unsupported") {
		t.Fatalf("did not expect graphql_action_export_unsupported finding, got %+v", res.Findings)
	}
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Unsupported\n\nNo unsupported export findings.")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "type Query {\n}")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "type Mutation")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "schema.graphqls"), "approveTransfer(input: JSON!): JSON! @auth")
	assertFileContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *mutationResolver) ApproveTransfer")
	assertFileNotContains(t, filepath.Join(out, "app", "graphqlapi", "resolver", "schema.resolvers.go"), "func (r *Resolver) Query()")
	assertFileContains(t, filepath.Join(out, "database", "policies", "support.go"), `{Name: "approveTransfer"}`)
	assertStandaloneNoPickleRuntime(t, out)
	writeExportedSupportedGraphQLActionPolicyStateTest(t, out)
	writeExportedActionOnlyGraphQLActionResolverTest(t, out)
	writeExportedActionOnlyGraphQLActionHTTPTest(t, out)
	runExported(t, out, "go", "test", "./...")
}

func writeExportedUnsupportedGraphQLActionPolicyStateTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package policies

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedPolicyStateOmitsUnsupportedGraphQLActions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}
	var actionRows int64
	if err := db.Table("graphql_actions").Where("name = ?", "approveTransfer").Count(&actionRows).Error; err != nil {
		t.Fatal(err)
	}
	if actionRows != 0 {
		t.Fatalf("unsupported graphql action rows = %d, want 0", actionRows)
	}
	var exposureRows int64
	if err := db.Table("graphql_exposures").Where("model = ? AND operation = ?", "users", "list").Count(&exposureRows).Error; err != nil {
		t.Fatal(err)
	}
	if exposureRows != 1 {
		t.Fatalf("supported graphql exposure rows = %d, want 1", exposureRows)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "policies", "exported_unsupported_graphql_action_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedSupportedGraphQLActionPolicyStateTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package policies

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportedPolicyStateSeedsSupportedGraphQLActions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db, "sqlite"); err != nil {
		t.Fatalf("policy migrate: %v", err)
	}
	var actionRows int64
	if err := db.Table("graphql_actions").Where("name = ?", "approveTransfer").Count(&actionRows).Error; err != nil {
		t.Fatal(err)
	}
	if actionRows != 1 {
		t.Fatalf("supported graphql action rows = %d, want 1", actionRows)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "database", "policies", "exported_supported_graphql_action_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedSupportedGraphQLActionResolverTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package resolver

import (
	"context"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

func TestExportedGraphQLControllerActionResolverCallsController(t *testing.T) {
	mutations := &mutationResolver{Resolver: &Resolver{}}
	ctx := WithGraphQLAPIAuthClaims(context.Background(), &GraphQLAPIAuthClaims{
		UserID:     "user-1",
		Role:       "viewer",
		Roles:      []string{"tenant_admin"},
		Manages:    true,
		RBACLoaded: true,
	})
	got, err := mutations.ApproveTransfer(ctx, map[string]any{"note": "ship it"})
	if err != nil {
		t.Fatalf("approveTransfer: %v", err)
	}
	if got["ok"] != true || got["userId"] != "user-1" || got["role"] != "tenant_admin" {
		t.Fatalf("approveTransfer response = %#v", got)
	}
	if _, err := mutations.ApproveTransfer(context.Background(), map[string]any{}); err == nil {
		t.Fatal("approveTransfer without auth should fail")
	}
	if _, err := mutations.ApproveTransfer(ctx, map[string]any{"note": strings.Repeat("x", maxGraphQLAPIActionInputStringBytes+1)}); !isControllerActionBadInput(err) {
		t.Fatalf("oversized action input error = %v, want BAD_USER_INPUT", err)
	}
	if _, err := mutations.ApproveTransfer(ctx, map[string]any{"nested": map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": map[string]any{"f": map[string]any{"g": map[string]any{"h": map[string]any{"i": "too deep"}}}}}}}}}}); !isControllerActionBadInput(err) {
		t.Fatalf("deep action input error = %v, want BAD_USER_INPUT", err)
	}
}

func isControllerActionBadInput(err error) bool {
	gqlErr, ok := err.(*gqlerror.Error)
	return ok && gqlErr.Extensions["code"] == "BAD_USER_INPUT" && gqlErr.Message == "GraphQL action input exceeds safety limits"
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "resolver", "exported_controller_action_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedActionOnlyGraphQLActionResolverTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package resolver

import (
	"context"
	"testing"
)

func TestExportedActionOnlyGraphQLControllerActionResolverCallsController(t *testing.T) {
	mutations := &mutationResolver{Resolver: &Resolver{}}
	ctx := WithGraphQLAPIAuthClaims(context.Background(), &GraphQLAPIAuthClaims{
		UserID: "user-1",
		Role:   "viewer",
	})
	got, err := mutations.ApproveTransfer(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("approveTransfer: %v", err)
	}
	if got["ok"] != true || got["userId"] != "user-1" {
		t.Fatalf("approveTransfer response = %#v", got)
	}
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "resolver", "exported_action_only_controller_action_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExportedActionOnlyGraphQLActionHTTPTest(t *testing.T, out string) {
	t.Helper()
	testSrc := `package graphqlapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"basic-crud/app/graphqlapi"
	"basic-crud/app/http/auth"
	"basic-crud/app/http/auth/jwt"
	"basic-crud/app/models"
)

func TestExportedActionOnlyGraphQLHandlerServesControllerAction(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	models.SetDB(db)
	if err := db.AutoMigrate(&models.JwtToken{}); err != nil {
		t.Fatalf("auto migrate jwt tokens: %v", err)
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

	handler := graphqlapi.Handler()
	body := []byte(` + "`" + `{"query":"mutation Approve($input: JSON!) { approveTransfer(input: $input) }","variables":{"input":{"note":"ship it"}}}` + "`" + `)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !actionResponseHasErrorCode(t, rec.Body.Bytes(), "UNAUTHENTICATED") {
		t.Fatalf("unauthenticated mutation should be denied, body=%s", rec.Body.String())
	}

	token, err := auth.Driver("jwt").(*jwt.Driver).SignToken(jwt.Claims{
		Subject:   uuid.NewString(),
		Role:      "viewer",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			ApproveTransfer map[string]any ` + "`" + `json:"approveTransfer"` + "`" + `
		} ` + "`" + `json:"data"` + "`" + `
		Errors []map[string]any ` + "`" + `json:"errors"` + "`" + `
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode authenticated response: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Errors) != 0 || resp.Data.ApproveTransfer["ok"] != true || resp.Data.ApproveTransfer["userId"] == "" {
		t.Fatalf("authenticated mutation response = %s", rec.Body.String())
	}

	oversizedBody, err := json.Marshal(map[string]any{
		"query": "mutation Approve($input: JSON!) { approveTransfer(input: $input) }",
		"variables": map[string]any{
			"input": map[string]any{"note": strings.Repeat("x", 4097)},
		},
	})
	if err != nil {
		t.Fatalf("marshal oversized action input: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(oversizedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("oversized action input status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !actionResponseHasErrorCode(t, rec.Body.Bytes(), "BAD_USER_INPUT") {
		t.Fatalf("oversized action input should be denied, body=%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), strings.Repeat("x", 128)) {
		t.Fatalf("oversized action input leaked value: %s", rec.Body.String())
	}
}

func actionResponseHasErrorCode(t *testing.T, body []byte, code string) bool {
	t.Helper()
	var resp struct {
		Errors []struct {
			Extensions map[string]any ` + "`" + `json:"extensions"` + "`" + `
		} ` + "`" + `json:"errors"` + "`" + `
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, string(body))
	}
	for _, gqlErr := range resp.Errors {
		if gqlErr.Extensions["code"] == code {
			return true
		}
	}
	return false
}
`
	if err := os.WriteFile(filepath.Join(out, "app", "graphqlapi", "exported_action_only_handler_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExporterReportsGraphQLControllerActionsAsUnsupported(t *testing.T) {
	dir := t.TempDir()
	policyDir := filepath.Join(dir, "database", "policies", "graphql")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "2026_06_05_100000_actions.go"), []byte(`package graphql

func (p *API) Up() {
	p.ControllerAction("approveTransfer", nil)
}
`), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	ex := &exporter{
		project: &generator.Project{Dir: dir},
		result:  &Result{},
	}
	ex.addGraphQLActionFindings()
	if !hasFinding(ex.result.Findings, "graphql_action_export_unsupported") {
		t.Fatalf("expected graphql_action_export_unsupported finding, got %+v", ex.result.Findings)
	}
	if got := ex.result.Findings[0].File; got != filepath.Join("database", "policies", "graphql") {
		t.Fatalf("finding file = %q, want GraphQL policy directory", got)
	}
	if got := ex.result.Findings[0].Message; !strings.Contains(got, "approveTransfer") {
		t.Fatalf("finding message %q does not name controller action", got)
	}
}

func TestFindingCategoryClassifiesUnlowerableBoundariesAsUnsupported(t *testing.T) {
	if got := findingCategory("graphql_action_export_unsupported"); got != "unsupported" {
		t.Fatalf("findingCategory(graphql_action_export_unsupported) = %q, want unsupported", got)
	}
	if got := findingCategory("orm_export_unsupported"); got != "unsupported" {
		t.Fatalf("findingCategory(orm_export_unsupported) = %q, want unsupported", got)
	}
	if got := findingCategory("query_export_unsupported"); got != "unsupported" {
		t.Fatalf("findingCategory(query_export_unsupported) = %q, want unsupported", got)
	}
	if got := findingCategory("migration_export_unsupported"); got != "unsupported" {
		t.Fatalf("findingCategory(migration_export_unsupported) = %q, want unsupported", got)
	}
	if got := findingCategory("action_export_unsupported_signature"); got != "unsupported" {
		t.Fatalf("findingCategory(action_export_unsupported_signature) = %q, want unsupported", got)
	}
	if got := findingCategory("action_export_unsupported_query"); got != "unsupported" {
		t.Fatalf("findingCategory(action_export_unsupported_query) = %q, want unsupported", got)
	}
	if got := findingCategory("gate_export_unsupported_signature"); got != "unsupported" {
		t.Fatalf("findingCategory(gate_export_unsupported_signature) = %q, want unsupported", got)
	}
	for _, rule := range []string{
		"actions_audit",
		"gate_export_policy_dependency",
		"gate_export_dynamic_role",
		"gate_export_callsite",
		"raw_sql_migration",
		"new_unclassified_export_boundary",
	} {
		if got := findingCategory(rule); got != "manual" {
			t.Fatalf("findingCategory(%q) = %q, want manual", rule, got)
		}
	}
}

func TestExportReportTreatsUnclassifiedFindingsAsManualReview(t *testing.T) {
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
				Rule:    "new_unclassified_export_boundary",
				Message: "new exporter note",
			}},
		},
	}
	if err := ex.writeReport("gorm"); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	assertFileContains(t, reportPath, "## Unsupported\n\nNo unsupported export findings.")
	assertFileContains(t, reportPath, "## Manual Review")
	assertFileContains(t, reportPath, "`database/migrations` `new_unclassified_export_boundary` - new exporter note")
}

func TestRewriteUnsupportedQueryMethodReportsExportBoundary(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Index(sql string) ([]models.User, error) {
	return models.QueryUser().Raw(sql).All()
}
`)
	_, err := ex.rewriteGoFile("controller.go", src)
	if err == nil {
		t.Fatal("rewriteGoFile succeeded for unsupported query method")
	}
	got := err.Error()
	assertContainsAll(t, got,
		"controller.go:",
		"[query_export_unsupported]",
		"unsupported query method Raw",
	)
}

func TestRewriteQueryWithoutTerminalReportsExportBoundary(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Index(role string) any {
	return models.QueryUser().WhereRole(role)
}
`)
	_, err := ex.rewriteGoFile("controller.go", src)
	if err == nil {
		t.Fatal("rewriteGoFile succeeded for query chain without terminal operation")
	}
	got := err.Error()
	assertContainsAll(t, got,
		"controller.go:",
		"[query_export_unsupported]",
		"query chain has no terminal operation",
	)
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
		`q = q.Select("*")`,
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

func TestRewriteQueryVisibilitySelectors(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true, "Post": true},
		schemaTables: map[string]*schema.Table{
			"User": {Name: "users", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String, IsPublic: true},
				{Name: "email", Type: schema.String, IsPublic: true, IsEncrypted: true},
				{Name: "private_key", Type: schema.String, IsOwnerSees: true, IsSealed: true},
				{Name: "password_hash", Type: schema.String, IsEncrypted: true},
			}},
			"Post": {Name: "posts", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true, IsPublic: true},
				{Name: "title", Type: schema.String, IsPublic: true},
				{Name: "body", Type: schema.Text, IsOwnerSees: true},
				{Name: "admin_note", Type: schema.String},
			}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func PublicUsers(name string) ([]models.User, error) {
	return models.QueryUser().
		SelectPublic().
		WhereName(name).
		All()
}

func OwnerPosts() ([]models.Post, error) {
	return models.QueryPost().
		SelectOwner().
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
		`Select([]string{"name", "email_encrypted"`,
		`Where("name = ?", name`,
		`Select([]string{"id", "title", "body"`,
	)
	for _, unexpected := range []string{"SelectPublic", "SelectOwner", "private_key", "password_hash", "admin_note"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained unsafe visibility detail %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableQueryVisibilitySelectors(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
		schemaTables: map[string]*schema.Table{
			"User": {Name: "users", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String, IsPublic: true},
				{Name: "email", Type: schema.String, IsPublic: true, IsEncrypted: true},
				{Name: "private_key", Type: schema.String, IsOwnerSees: true, IsSealed: true},
				{Name: "password_hash", Type: schema.String, IsEncrypted: true},
			}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func OwnerUsers(role string) ([]models.User, error) {
	q := models.QueryUser()
	q.SelectOwner()
	q.WhereRole(role)
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
		`q = q.Select([]string{"name", "email_encrypted", "private_key_encrypted"`,
		`q = q.Where("role = ?", role`,
		`return func() ([]models.User, error)`,
	)
	for _, unexpected := range []string{"SelectOwner", "password_hash"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten mutable source retained unsafe visibility detail %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteQueryRoleVisibilitySelectors(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
		managesRoles: map[string]bool{"admin": true},
		schemaTables: map[string]*schema.Table{
			"User": {Name: "users", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true, IsPublic: true},
				{Name: "name", Type: schema.String, IsPublic: true},
				{Name: "email", Type: schema.String, IsEncrypted: true, VisibleTo: map[string]bool{"support": true}},
				{Name: "private_key", Type: schema.String, IsSealed: true, IsOwnerSees: true},
				{Name: "password_hash", Type: schema.String, IsEncrypted: true},
			}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func RoleUsers(role string) ([]models.User, error) {
	return models.QueryUser().
		SelectFor(role).
		All()
}

func MultiRoleUsers(roles []string) ([]models.User, error) {
	return models.QueryUser().
		SelectForRoles(roles).
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
		`RoleVisibilitySelectScope`,
		`[]string{"id", "name"`,
		`"support": []string{"email_encrypted"`,
		`[]string{"admin"`,
		`[]string{role}`,
		`roles, false`,
	)
	for _, unexpected := range []string{"SelectFor", "password_hash"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten role visibility source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableQueryRoleVisibilitySelectors(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"User": true},
		managesRoles: map[string]bool{"admin": true},
		schemaTables: map[string]*schema.Table{
			"User": {Name: "users", Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true, IsPublic: true},
				{Name: "name", Type: schema.String, IsPublic: true},
				{Name: "email", Type: schema.String, IsEncrypted: true, VisibleTo: map[string]bool{"support": true}},
				{Name: "private_key", Type: schema.String, IsSealed: true, IsOwnerSees: true},
				{Name: "password_hash", Type: schema.String, IsEncrypted: true},
			}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func OwnerUsers(roles []string) ([]models.User, error) {
	q := models.QueryUser()
	q.SelectForOwner(roles)
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
		`q = q.Scopes(models.RoleVisibilitySelectScope`,
		`[]string{"id", "name"`,
		`"support": []string{"email_encrypted"`,
		`[]string{"id", "name", "private_key_encrypted"`,
		`roles, true`,
	)
	for _, unexpected := range []string{"SelectForOwner", "password_hash"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten mutable role visibility source retained %q:\n%s", unexpected, got)
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

func TestRewriteIntegrityQueryVerificationHelpers(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Transaction": true},
		integrityModels: map[string]integrityModelInfo{
			"Transaction": {
				Table:      &schema.Table{Name: "transactions"},
				AppendOnly: true,
			},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Verify(tx *models.Transaction) error {
	if err := models.QueryTransaction().VerifyRow(tx); err != nil {
		return err
	}
	return models.QueryTransaction().VerifyChain()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	assertContainsAll(t, strings.Join(strings.Fields(got), " "),
		`models.VerifyTransactionRow(tx)`,
		`models.VerifyTransactionChain()`,
	)
	for _, unexpected := range []string{"QueryTransaction", ".VerifyRow", ".VerifyChain"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
	}
}

func TestRewriteMutableIntegrityQueryVerificationHelpers(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Transaction": true},
		integrityModels: map[string]integrityModelInfo{
			"Transaction": {
				Table:      &schema.Table{Name: "transactions"},
				AppendOnly: true,
			},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Verify(tx *models.Transaction) error {
	q := models.QueryTransaction()
	if err := q.VerifyRow(tx); err != nil {
		return err
	}
	return q.VerifyChain()
}
`)
	out, err := ex.rewriteGoFile("controller.go", src)
	if err != nil {
		t.Fatalf("rewriteGoFile: %v", err)
	}
	got := string(out)
	assertContainsAll(t, strings.Join(strings.Fields(got), " "),
		`q := models.DB.Model(&models.`,
		`Transaction{})`,
		`models.VerifyTransactionRow(`,
		`tx`,
		`models.VerifyTransactionChain()`,
	)
	for _, unexpected := range []string{"QueryTransaction", ".VerifyRow", ".VerifyChain"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("rewritten source retained %q:\n%s", unexpected, got)
		}
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

func TestRewritePreservesTransactionCustomScopeQueries(t *testing.T) {
	ex := &exporter{
		sourceModule: "example.com/app",
		modulePath:   "exported-app",
		models:       map[string]bool{"Job": true},
		scopes: map[string][]generator.ScopeDef{
			"job": {{Name: "Ready"}},
		},
	}
	src := []byte(`package controllers

import "example.com/app/app/models"

func Claim() error {
	return models.WithTransaction(func(tx *models.Tx) error {
		jobs, err := tx.QueryJob().Ready().All()
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
	assertContainsAll(t, got, "tx.QueryJob().Ready().All()")
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
			if os.IsNotExist(err) {
				return nil
			}
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
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertStandaloneNoPickleRuntime(t *testing.T, out string) {
	t.Helper()
	assertNoExportFileContains(t, out, "github.com/shortontech/pickle")
	assertNoExportFileContains(t, out, "PICKLE_")
	assertNoExportFileContains(t, out, "RegisterPickleEndpoints")
	assertNoExportFileContains(t, out, "/pickle/config/reload")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	assertNoGoFileContains(t, out, "PICKLE_")
	assertNoGoFileContains(t, out, "RegisterPickleEndpoints")
	assertNoGoFileContains(t, out, "/pickle/config/reload")
	assertNoGoListDependency(t, out, "github.com/shortontech/pickle")
}

func assertNoExportFileContains(t *testing.T, root, needle string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			default:
				return nil
			}
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
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertNoGoFilesUnder(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		t.Fatalf("expected no exported Go files under %s, found %s", root, path)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertNoGoListDependency(t *testing.T, dir, modulePath string) {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./... failed: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == modulePath || strings.HasPrefix(dep, modulePath+"/") {
			t.Fatalf("exported module depends on %s via %s", modulePath, dep)
		}
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
