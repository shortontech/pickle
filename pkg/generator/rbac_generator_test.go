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

	if len(entries) != 5 {
		t.Fatalf("expected 5 files (4 migrations + types_gen.go), got %d", len(entries))
	}

	expected := []string{
		"2026_03_23_000001_create_roles_table_gen.go",
		"2026_03_23_000002_create_role_actions_table_gen.go",
		"2026_03_23_000003_create_role_user_table_gen.go",
		"2026_03_23_000004_create_rbac_changelog_table_gen.go",
		"types_gen.go",
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
	if !strings.Contains(string(data), "package rbac") {
		t.Error("expected package name substitution to 'rbac'")
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
	// 1 user file + 3 gen files + types_gen.go = 5
	if len(entries) != 5 {
		t.Errorf("expected 5 files (1 user + 3 gen + types_gen.go), got %d", len(entries))
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

	if len(entries) != 4 {
		t.Fatalf("expected 4 files (3 migrations + types_gen.go), got %d", len(entries))
	}

	expected := []string{
		"2026_03_25_000001_create_graphql_changelog_table_gen.go",
		"2026_03_25_000002_create_graphql_exposures_table_gen.go",
		"2026_03_25_000003_create_graphql_actions_table_gen.go",
		"types_gen.go",
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
	if !strings.Contains(content, `"name"`) {
		t.Error("expected name column")
	}
	if !strings.Contains(content, `"manages"`) {
		t.Error("expected manages column")
	}
	if !strings.Contains(content, `"birth_policy"`) {
		t.Error("expected birth_policy column")
	}
}

func TestRBACMigrationRoleUserCompositeKey(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACMigrations(dir, "mypkg"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "rbac", "2026_03_23_000003_create_role_user_table_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, `OnDelete("CASCADE")`) {
		t.Error("expected OnDelete(CASCADE) on foreign keys")
	}
	if !strings.Contains(content, `PrimaryKey("user_id", "role_id")`) {
		t.Error("expected composite PrimaryKey on user_id, role_id")
	}
}

func TestRBACMigrationChangelogNoSize(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACMigrations(dir, "mypkg"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "rbac", "2026_03_23_000004_create_rbac_changelog_table_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Should use String("id") without size, not String("id", 255)
	if strings.Contains(content, `String("id", 255)`) {
		t.Error("rbac_changelog id should use String without explicit size")
	}
	if !strings.Contains(content, `String("id").PrimaryKey()`) {
		t.Error("expected String(\"id\").PrimaryKey()")
	}
}

func TestWriteRBACModels(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACModels(dir); err != nil {
		t.Fatalf("WriteRBACModels: %v", err)
	}

	authDir := filepath.Join(dir, "auth")
	entries, err := os.ReadDir(authDir)
	if err != nil {
		t.Fatalf("reading auth dir: %v", err)
	}

	// 4 model files + pickle_gen.go = 5
	if len(entries) != 5 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected 5 files (4 models + pickle_gen.go), got %d: %v", len(entries), names)
	}

	expected := []string{
		"pickle_gen.go",
		"role_gen.go",
		"role_query_gen.go",
		"role_user_gen.go",
		"role_user_query_gen.go",
	}
	for i, e := range entries {
		if e.Name() != expected[i] {
			t.Errorf("expected %q, got %q", expected[i], e.Name())
		}
	}

	// Check package name substitution
	data, err := os.ReadFile(filepath.Join(authDir, "role_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package auth") {
		t.Error("expected package auth")
	}
	if strings.Contains(string(data), "__PACKAGE__") {
		t.Error("package placeholder not replaced")
	}
}

func TestWriteRBACModelsOverride(t *testing.T) {
	dir := t.TempDir()
	authDir := filepath.Join(dir, "auth")
	os.MkdirAll(authDir, 0o755)

	// User override for role.go suppresses role_gen.go
	userFile := filepath.Join(authDir, "role.go")
	os.WriteFile(userFile, []byte("package auth\n// user override"), 0o644)

	if err := WriteRBACModels(dir); err != nil {
		t.Fatalf("WriteRBACModels: %v", err)
	}

	genFile := filepath.Join(authDir, "role_gen.go")
	if _, err := os.Stat(genFile); err == nil {
		t.Error("role_gen.go should not exist when user override role.go exists")
	}

	// Other files should still be generated
	if _, err := os.Stat(filepath.Join(authDir, "role_user_gen.go")); err != nil {
		t.Error("role_user_gen.go should still be generated")
	}
}

func TestRBACModelRoleHasCorrectFields(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACModels(dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "auth", "role_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, field := range []string{"ID", "Slug", "Name", "Manages", "IsDefault", "BirthPolicy", "CreatedAt", "UpdatedAt"} {
		if !strings.Contains(content, field) {
			t.Errorf("Role model missing field %s", field)
		}
	}
}

func TestRBACModelRoleUserQueryScopes(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRBACModels(dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "auth", "role_user_query_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	for _, scope := range []string{"WhereUserID", "WhereRoleID", "WithRole"} {
		if !strings.Contains(content, scope) {
			t.Errorf("RoleUser query missing scope %s", scope)
		}
	}
}
