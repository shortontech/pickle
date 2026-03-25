package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRBACMigrations(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACMigrations(dir, "migrations"); err != nil {
		t.Fatalf("WriteRBACMigrations: %v", err)
	}

	rbacDir := filepath.Join(dir, "rbac")
	entries, err := os.ReadDir(rbacDir)
	if err != nil {
		t.Fatalf("reading rbac dir: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 migration files, got %d", len(entries))
	}

	expected := []string{
		"2026_03_23_000001_create_roles_table_gen.go",
		"2026_03_23_000002_create_role_actions_table_gen.go",
		"2026_03_23_000003_create_role_user_table_gen.go",
		"2026_03_23_000004_create_rbac_changelog_table_gen.go",
	}
	for i, e := range entries {
		if e.Name() != expected[i] {
			t.Errorf("expected %q, got %q", expected[i], e.Name())
		}
	}

	// Check package name substitution
	data, err := os.ReadFile(filepath.Join(rbacDir, expected[0]))
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

func TestWriteRBACMigrationsOverride(t *testing.T) {
	dir := t.TempDir()
	rbacDir := filepath.Join(dir, "rbac")
	os.MkdirAll(rbacDir, 0o755)

	// Create a user override
	userFile := filepath.Join(rbacDir, "2026_03_23_000001_create_roles_table.go")
	os.WriteFile(userFile, []byte("package migrations\n// user override"), 0o644)

	if err := WriteRBACMigrations(dir, "migrations"); err != nil {
		t.Fatalf("WriteRBACMigrations: %v", err)
	}

	// _gen.go version should not exist
	genFile := filepath.Join(rbacDir, "2026_03_23_000001_create_roles_table_gen.go")
	if _, err := os.Stat(genFile); err == nil {
		t.Error("_gen.go should not exist when user override exists")
	}

	// Other files should still be generated
	entries, err := os.ReadDir(rbacDir)
	if err != nil {
		t.Fatal(err)
	}
	// 1 user file + 3 gen files = 4
	if len(entries) != 4 {
		t.Errorf("expected 4 files (1 user + 3 gen), got %d", len(entries))
	}
}

func TestWriteGraphQLMigrations(t *testing.T) {
	dir := t.TempDir()
	if err := WriteGraphQLMigrations(dir, "migrations"); err != nil {
		t.Fatalf("WriteGraphQLMigrations: %v", err)
	}

	gqlDir := filepath.Join(dir, "graphql")
	entries, err := os.ReadDir(gqlDir)
	if err != nil {
		t.Fatalf("reading graphql dir: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 migration files, got %d", len(entries))
	}

	expected := []string{
		"2026_03_25_000001_create_graphql_changelog_table_gen.go",
		"2026_03_25_000002_create_graphql_exposures_table_gen.go",
		"2026_03_25_000003_create_graphql_actions_table_gen.go",
	}
	for i, e := range entries {
		if e.Name() != expected[i] {
			t.Errorf("expected %q, got %q", expected[i], e.Name())
		}
	}
}

func TestWriteGraphQLMigrationsOverride(t *testing.T) {
	dir := t.TempDir()
	gqlDir := filepath.Join(dir, "graphql")
	os.MkdirAll(gqlDir, 0o755)

	// Create a user override
	userFile := filepath.Join(gqlDir, "2026_03_25_000001_create_graphql_changelog_table.go")
	os.WriteFile(userFile, []byte("package migrations\n// user override"), 0o644)

	if err := WriteGraphQLMigrations(dir, "migrations"); err != nil {
		t.Fatalf("WriteGraphQLMigrations: %v", err)
	}

	genFile := filepath.Join(gqlDir, "2026_03_25_000001_create_graphql_changelog_table_gen.go")
	if _, err := os.Stat(genFile); err == nil {
		t.Error("_gen.go should not exist when user override exists")
	}
}

func TestRBACMigrationContainsRolesTable(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACMigrations(dir, "mypkg"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "rbac", "2026_03_23_000001_create_roles_table_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, `"roles"`) {
		t.Error("expected roles table creation")
	}
	if !strings.Contains(content, `"slug"`) {
		t.Error("expected slug column")
	}
	if !strings.Contains(content, `"display_name"`) {
		t.Error("expected display_name column")
	}
	if !strings.Contains(content, `"is_manages"`) {
		t.Error("expected is_manages column")
	}
	if !strings.Contains(content, `"birth_policy"`) {
		t.Error("expected birth_policy column")
	}
}
