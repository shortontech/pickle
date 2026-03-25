package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAuditMigrations(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAuditMigrations(dir, "migrations"); err != nil {
		t.Fatalf("WriteAuditMigrations: %v", err)
	}

	auditDir := filepath.Join(dir, "audit")
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatalf("reading audit dir: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 migration files, got %d", len(entries))
	}

	expected := []string{
		"2026_03_25_000001_create_model_types_table_gen.go",
		"2026_03_25_000002_create_action_types_table_gen.go",
		"2026_03_25_000003_create_user_actions_table_gen.go",
	}
	for i, e := range entries {
		if e.Name() != expected[i] {
			t.Errorf("expected %q, got %q", expected[i], e.Name())
		}
	}

	// Check package name substitution
	data, err := os.ReadFile(filepath.Join(auditDir, expected[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package migrations") {
		t.Error("expected package name substitution")
	}
	if strings.Contains(string(data), "__PACKAGE__") {
		t.Error("package placeholder not replaced")
	}
}

func TestWriteAuditMigrationsOverride(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit")
	os.MkdirAll(auditDir, 0o755)

	// Create a user override
	userFile := filepath.Join(auditDir, "2026_03_25_000001_create_model_types_table.go")
	os.WriteFile(userFile, []byte("package migrations\n// user override"), 0o644)

	if err := WriteAuditMigrations(dir, "migrations"); err != nil {
		t.Fatalf("WriteAuditMigrations: %v", err)
	}

	// _gen.go version should not exist
	genFile := filepath.Join(auditDir, "2026_03_25_000001_create_model_types_table_gen.go")
	if _, err := os.Stat(genFile); err == nil {
		t.Error("_gen.go should not exist when user override exists")
	}

	// Other files should still be generated
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	// 1 user file + 2 gen files = 3
	if len(entries) != 3 {
		t.Errorf("expected 3 files (1 user + 2 gen), got %d", len(entries))
	}
}

func TestAuditMigrationContainsModelTypesTable(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAuditMigrations(dir, "mypkg"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit", "2026_03_25_000001_create_model_types_table_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, `"model_types"`) {
		t.Error("expected model_types table creation")
	}
	if !strings.Contains(content, `"name"`) {
		t.Error("expected name column")
	}
}

func TestAuditMigrationContainsUserActionsTable(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAuditMigrations(dir, "mypkg"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit", "2026_03_25_000003_create_user_actions_table_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, `"user_actions"`) {
		t.Error("expected user_actions table creation")
	}
	if !strings.Contains(content, `"user_id"`) {
		t.Error("expected user_id column")
	}
	if !strings.Contains(content, `"action_type_id"`) {
		t.Error("expected action_type_id column")
	}
	if !strings.Contains(content, `"ip_address"`) {
		t.Error("expected ip_address column")
	}
	if !strings.Contains(content, `"request_id"`) {
		t.Error("expected request_id column")
	}
}
