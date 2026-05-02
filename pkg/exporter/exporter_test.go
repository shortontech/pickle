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
	if !hasFinding(res.Findings, "generated_auth") {
		t.Fatalf("expected generated_auth finding, got %+v", res.Findings)
	}
	if !hasFinding(res.Findings, "generated_policies") {
		t.Fatalf("expected generated_policies finding, got %+v", res.Findings)
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
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "generated_auth")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Omitted")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func Env(key, fallback string) string")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "type ConnectionConfig struct")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func OpenGORM(conn ConnectionConfig) *gorm.DB")
	assertFileContains(t, filepath.Join(out, "config", "app.go"), "func app() AppConfig")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "models.SetDB(config.Database.OpenGORM())")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "crypto/hmac")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "ErrInvalidToken")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "jwt.NewDriver(config.Env)")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "func DefaultAuthMiddleware")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "func ActiveDriverName")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "requires manual implementation after export")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_ban.go"), "DB.Save(user).Error")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_ban_gate_gen.go"), `HasAnyRole("admin")`)
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "func (m *User) Ban")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_actions.go"), "CanBan(ctx, m)")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "models.DB.Model(&models.User{})")
	assertNoGoFileContains(t, out, "QueryUser")
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "user_controller.go"), "basic-crud/internal/httpx")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "Target ORM: `gorm`")

	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "pickle.")
	assertNoGoFileContains(t, out, "Pickle")
	assertNoGoFileContains(t, out, "PICKLE_")
	assertFileContains(t, filepath.Join(out, "go.sum"), "gorm.io/gorm")
	runExported(t, out, "go", "test", "./...")
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
	policy := `package policies

type GrantBan_2026_03_24_100000 struct { Policy }

func (m *GrantBan_2026_03_24_100000) Up() { m.AlterRole("admin").Can("Ban") }
func (m *GrantBan_2026_03_24_100000) Down() { m.AlterRole("admin").RevokeCan("Ban") }
`
	if err := os.WriteFile(filepath.Join(dir, "ban.go"), []byte(action), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "database", "policies", "2026_03_24_100000_grant_ban.go"), []byte(policy), 0o644); err != nil {
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
	assertFileContains(t, filepath.Join(out, "app", "http", "controllers", "account_controller.go"), "models.DB.Model(&models.Account{})")
	assertPathMissing(t, filepath.Join(out, "integrity_test.go"))
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	assertNoGoFileContains(t, out, "QueryAccount")
	runExported(t, out, "go", "test", "./...")
}

func TestExportEncryptionCompilesWithFinding(t *testing.T) {
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
	if !hasFinding(res.Findings, "encrypted_columns") {
		t.Fatalf("expected encrypted_columns finding, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "func (m *User) Public() UserPublic")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "func PublicUsers(records []User) []UserPublic")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Manual Review")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "encrypted_columns")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
	runExported(t, out, "go", "test", "./...")
}

func TestExportZeroGraphQLCompilesWithFinding(t *testing.T) {
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
	if !hasFinding(res.Findings, "generated_graphql") {
		t.Fatalf("expected generated_graphql finding, got %+v", res.Findings)
	}

	assertPathMissing(t, filepath.Join(out, "app", "graphql"))
	assertFileContains(t, filepath.Join(out, "app", "http", "requests", "bindings.go"), "package requests")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "generated_graphql")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
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

func TestExportCronCompilesWithFinding(t *testing.T) {
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
	if !hasFinding(res.Findings, "generated_jobs") {
		t.Fatalf("expected generated_jobs finding, got %+v", res.Findings)
	}
	if !hasFinding(res.Findings, "generated_commands") {
		t.Fatalf("expected generated_commands finding, got %+v", res.Findings)
	}

	assertFileContains(t, filepath.Join(out, "app", "jobs", "support.go"), "type Scheduler struct")
	assertFileContains(t, filepath.Join(out, "schedule", "jobs.go"), "jobs.Cron")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "## Partial Support")
	assertFileContains(t, filepath.Join(out, "EXPORT_REPORT.md"), "generated_jobs")
	assertNoGoFileContains(t, out, "github.com/shortontech/pickle")
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
