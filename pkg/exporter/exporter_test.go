package exporter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
)

func TestExportBasicCRUDNoPickleImports(t *testing.T) {
	projectDir := copyProject(t, filepath.Join("..", "..", "testdata", "basic-crud"))
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
	if !hasFinding(res.Findings, "generated_policies") {
		t.Fatalf("expected generated_policies finding, got %+v", res.Findings)
	}
	assertFileContains(t, filepath.Join(out, "go.mod"), "gorm.io/gorm")
	assertFileContains(t, filepath.Join(out, "app", "models", "user.go"), "type User struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "user_post_stat.go"), "type UserPostStat struct")
	assertFileContains(t, filepath.Join(out, "app", "models", "db.go"), "var DB *gorm.DB")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "CREATE TABLE")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.down.sql"), "DROP TABLE")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260221100000_create_users_table.up.sql"), "CREATE INDEX")
	assertFileContains(t, filepath.Join(out, "database", "migrations", "20260228100000_create_user_post_stats_view.up.sql"), "CREATE VIEW")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func Env(key, fallback string) string")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "type ConnectionConfig struct")
	assertFileContains(t, filepath.Join(out, "config", "support.go"), "func OpenGORM(conn ConnectionConfig) *gorm.DB")
	assertFileContains(t, filepath.Join(out, "config", "app.go"), "func app() AppConfig")
	assertFileContains(t, filepath.Join(out, "cmd", "server", "main.go"), "models.SetDB(config.Database.OpenGORM())")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "crypto/hmac")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "jwt", "jwt.go"), "ErrInvalidToken")
	assertFileContains(t, filepath.Join(out, "app", "http", "auth", "auth.go"), "jwt.NewDriver(config.Env)")
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
