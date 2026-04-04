package generator

import (
	"encoding/json"
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

	if len(entries) != 4 {
		t.Fatalf("expected 4 files (types_gen.go + 3 migrations), got %d", len(entries))
	}

	expected := []string{
		"2026_03_25_000001_create_model_types_table_gen.go",
		"2026_03_25_000002_create_action_types_table_gen.go",
		"2026_03_25_000003_create_user_actions_table_gen.go",
		"types_gen.go",
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
	// 1 user file + 2 gen files + types_gen.go = 4
	if len(entries) != 4 {
		t.Errorf("expected 4 files (1 user + 2 gen + types_gen.go), got %d", len(entries))
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

func TestAuditMigrationActionTypesUniqueIndex(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAuditMigrations(dir, "mypkg"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "audit", "2026_03_25_000002_create_action_types_table_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "AddUniqueIndex") {
		t.Error("expected AddUniqueIndex on action_types")
	}
}

func TestWriteAuditModels(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAuditModels(dir); err != nil {
		t.Fatalf("WriteAuditModels: %v", err)
	}

	auditDir := filepath.Join(dir, "audit")
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatalf("reading audit dir: %v", err)
	}

	// pickle_gen.go + 7 model files = 8
	expected := map[string]bool{
		"pickle_gen.go":            true,
		"model_type_gen.go":        true,
		"model_type_query_gen.go":  true,
		"action_type_gen.go":       true,
		"action_type_query_gen.go": true,
		"user_action_gen.go":       true,
		"user_action_query_gen.go": true,
		"performed_gen.go":         true,
	}

	for _, e := range entries {
		if !expected[e.Name()] {
			t.Errorf("unexpected file: %s", e.Name())
		}
		delete(expected, e.Name())
	}
	for name := range expected {
		t.Errorf("missing expected file: %s", name)
	}
}

func TestWriteAuditModelsOverride(t *testing.T) {
	dir := t.TempDir()
	auditDir := filepath.Join(dir, "audit")
	os.MkdirAll(auditDir, 0o755)

	// Create user override for user_action
	os.WriteFile(filepath.Join(auditDir, "user_action.go"), []byte("package audit\n// custom"), 0o644)

	if err := WriteAuditModels(dir); err != nil {
		t.Fatal(err)
	}

	// gen file should not exist for overridden file
	if _, err := os.Stat(filepath.Join(auditDir, "user_action_gen.go")); err == nil {
		t.Error("user_action_gen.go should not exist when user override exists")
	}

	// Other gen files should exist
	if _, err := os.Stat(filepath.Join(auditDir, "model_type_gen.go")); err != nil {
		t.Error("model_type_gen.go should exist")
	}
}

func TestIDRegistry_StableIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")

	reg, err := LoadIDRegistry(path)
	if err != nil {
		t.Fatal(err)
	}

	id1 := reg.EnsureModelType("User")
	id2 := reg.EnsureModelType("Post")
	id3 := reg.EnsureActionType("User", "Ban")
	id4 := reg.EnsureActionType("User", "Suspend")
	id5 := reg.EnsureActionType("Post", "Publish")

	if id1 != 1 || id2 != 2 {
		t.Errorf("expected model type IDs 1, 2; got %d, %d", id1, id2)
	}
	if id3 != 1 || id4 != 2 || id5 != 3 {
		t.Errorf("expected action type IDs 1, 2, 3; got %d, %d, %d", id3, id4, id5)
	}

	// Save and reload
	if err := SaveIDRegistry(path, reg); err != nil {
		t.Fatal(err)
	}

	reg2, err := LoadIDRegistry(path)
	if err != nil {
		t.Fatal(err)
	}

	// IDs must be stable
	if reg2.EnsureModelType("User") != 1 {
		t.Error("User model type ID changed after reload")
	}
	if reg2.EnsureActionType("User", "Ban") != 1 {
		t.Error("User.Ban action type ID changed after reload")
	}

	// New entry gets next ID
	id6 := reg2.EnsureActionType("Post", "Archive")
	if id6 != 4 {
		t.Errorf("expected new action type ID 4, got %d", id6)
	}
}

func TestIDRegistry_RemovedActionKeepsID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")

	reg, err := LoadIDRegistry(path)
	if err != nil {
		t.Fatal(err)
	}

	reg.EnsureActionType("User", "Ban")    // ID 1
	reg.EnsureActionType("User", "Suspend") // ID 2

	if err := SaveIDRegistry(path, reg); err != nil {
		t.Fatal(err)
	}

	// Reload — simulate removing Ban from codebase (don't call EnsureActionType for it)
	reg2, err := LoadIDRegistry(path)
	if err != nil {
		t.Fatal(err)
	}

	// Re-add Ban — should get same ID
	id := reg2.EnsureActionType("User", "Ban")
	if id != 1 {
		t.Errorf("re-added action should get original ID 1, got %d", id)
	}
}

func TestGenerateAuditSeed(t *testing.T) {
	reg := &IDRegistry{
		NextModelTypeID:  3,
		NextActionTypeID: 4,
		ModelTypes:       map[string]int{"User": 1, "Post": 2},
		ActionTypes: map[string]ActionReg{
			"User.Ban":      {ID: 1, ModelTypeID: 1, Model: "User", Action: "Ban"},
			"User.Suspend":  {ID: 2, ModelTypeID: 1, Model: "User", Action: "Suspend"},
			"Post.Publish":  {ID: 3, ModelTypeID: 2, Model: "Post", Action: "Publish"},
		},
	}

	src, err := GenerateAuditSeed(reg, "audit")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "SeedActionTypes_2026_03_25_000004") {
		t.Error("expected seed struct name")
	}
	if !strings.Contains(content, "INSERT INTO model_types") {
		t.Error("expected model_types INSERT")
	}
	if !strings.Contains(content, "INSERT INTO action_types") {
		t.Error("expected action_types INSERT")
	}
	if !strings.Contains(content, "ON CONFLICT (id) DO NOTHING") {
		t.Error("expected idempotent insert")
	}
	if !strings.Contains(content, "'User'") {
		t.Error("expected User model type name")
	}
	if !strings.Contains(content, "'Ban'") {
		t.Error("expected Ban action type name")
	}
}

func TestGenerateAuditConstants(t *testing.T) {
	reg := &IDRegistry{
		ActionTypes: map[string]ActionReg{
			"User.Ban":     {ID: 1, ModelTypeID: 1, Model: "User", Action: "Ban"},
			"User.Suspend": {ID: 2, ModelTypeID: 1, Model: "User", Action: "Suspend"},
			"Post.Publish": {ID: 3, ModelTypeID: 2, Model: "Post", Action: "Publish"},
		},
	}

	src, err := GenerateAuditConstants(reg, "audit")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "ActionTypeUserBan") {
		t.Error("expected ActionTypeUserBan constant")
	}
	if !strings.Contains(content, "ActionTypeUserSuspend") {
		t.Error("expected ActionTypeUserSuspend constant")
	}
	if !strings.Contains(content, "ActionTypePostPublish") {
		t.Error("expected ActionTypePostPublish constant")
	}
	if !strings.Contains(content, "= 1") || !strings.Contains(content, "= 2") || !strings.Contains(content, "= 3") {
		t.Error("expected correct ID values")
	}
}

func TestIDRegistryJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.json")
	reg := &IDRegistry{
		NextModelTypeID:  2,
		NextActionTypeID: 2,
		ModelTypes:       map[string]int{"User": 1},
		ActionTypes: map[string]ActionReg{
			"User.Ban": {ID: 1, ModelTypeID: 1, Model: "User", Action: "Ban"},
		},
	}

	if err := SaveIDRegistry(path, reg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var parsed IDRegistry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.NextModelTypeID != 2 {
		t.Errorf("expected NextModelTypeID=2, got %d", parsed.NextModelTypeID)
	}
	if parsed.ModelTypes["User"] != 1 {
		t.Error("expected User model type ID 1")
	}
}
