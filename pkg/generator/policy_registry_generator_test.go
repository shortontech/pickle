package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanPolicyFilesEmpty(t *testing.T) {
	dir := t.TempDir()
	entries, err := ScanPolicyFiles(dir)
	if err != nil {
		t.Fatalf("ScanPolicyFiles: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestScanPolicyFilesNonExistent(t *testing.T) {
	entries, err := ScanPolicyFiles("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestScanPolicyFilesFindsPolicy(t *testing.T) {
	dir := t.TempDir()
	src := `package policies

type CreateInitialRoles_2026_03_23_100000 struct {
	Policy
}

func (m *CreateInitialRoles_2026_03_23_100000) Up() {
	m.CreateRole("admin").Name("Administrator").Manages()
}

func (m *CreateInitialRoles_2026_03_23_100000) Down() {
	m.DropRole("admin")
}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_03_23_100000_create_initial_roles.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ScanPolicyFiles(dir)
	if err != nil {
		t.Fatalf("ScanPolicyFiles: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "2026_03_23_100000_create_initial_roles" {
		t.Errorf("unexpected ID: %q", entries[0].ID)
	}
	if entries[0].StructName != "CreateInitialRoles_2026_03_23_100000" {
		t.Errorf("unexpected struct name: %q", entries[0].StructName)
	}
}

func TestScanGraphQLPolicyFilesFindsGraphQLPolicy(t *testing.T) {
	dir := t.TempDir()
	src := `package graphql

type ExposeUsers_2026_03_25_100000 struct {
	GraphQLPolicy
}

func (m *ExposeUsers_2026_03_25_100000) Up() {
	m.Expose("User", func(e *ExposeBuilder) { e.All() })
}

func (m *ExposeUsers_2026_03_25_100000) Down() {
	m.Unexpose("User")
}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_03_25_100000_expose_users.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ScanGraphQLPolicyFiles(dir)
	if err != nil {
		t.Fatalf("ScanGraphQLPolicyFiles: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "2026_03_25_100000_expose_users" {
		t.Errorf("unexpected ID: %q", entries[0].ID)
	}
}

func TestScanPolicyFilesSkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	// Create a graphql/ subdirectory — should be ignored by ScanPolicyFiles
	if err := os.MkdirAll(filepath.Join(dir, "graphql"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package graphql

type ExposeUsers_2026_03_25_100000 struct {
	GraphQLPolicy
}
`
	if err := os.WriteFile(filepath.Join(dir, "graphql", "2026_03_25_100000_expose_users.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := ScanPolicyFiles(dir)
	if err != nil {
		t.Fatalf("ScanPolicyFiles: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (subdirs should be ignored), got %d", len(entries))
	}
}

func TestScanPolicyFilesSortedByTimestamp(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct {
		name, structName string
	}{
		{"2026_03_23_200000_add_editor.go", "AddEditor_2026_03_23_200000"},
		{"2026_03_23_100000_create_admin.go", "CreateAdmin_2026_03_23_100000"},
	} {
		src := "package policies\n\ntype " + f.structName + " struct {\n\tPolicy\n}\n"
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ScanPolicyFiles(dir)
	if err != nil {
		t.Fatalf("ScanPolicyFiles: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != "2026_03_23_100000_create_admin" {
		t.Errorf("expected first entry to be create_admin, got %q", entries[0].ID)
	}
	if entries[1].ID != "2026_03_23_200000_add_editor" {
		t.Errorf("expected second entry to be add_editor, got %q", entries[1].ID)
	}
}

func TestGeneratePolicyRegistry(t *testing.T) {
	entries := []PolicyFileEntry{
		{ID: "2026_03_23_100000_create_roles", StructName: "CreateRoles_2026_03_23_100000"},
	}
	src, err := GeneratePolicyRegistry("policies", entries)
	if err != nil {
		t.Fatalf("GeneratePolicyRegistry: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "package policies") {
		t.Error("missing package declaration")
	}
	if !strings.Contains(s, "PolicyRegistry") {
		t.Error("missing PolicyRegistry var")
	}
	if !strings.Contains(s, "PolicyEntry") {
		t.Error("missing PolicyEntry type reference")
	}
	if !strings.Contains(s, "CreateRoles_2026_03_23_100000") {
		t.Error("missing struct name")
	}
}

func TestGenerateGraphQLPolicyRegistry(t *testing.T) {
	entries := []PolicyFileEntry{
		{ID: "2026_03_25_100000_expose_users", StructName: "ExposeUsers_2026_03_25_100000"},
	}
	src, err := GenerateGraphQLPolicyRegistry("graphql", entries)
	if err != nil {
		t.Fatalf("GenerateGraphQLPolicyRegistry: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "package graphql") {
		t.Error("missing package declaration")
	}
	if !strings.Contains(s, "GraphQLPolicyRegistry") {
		t.Error("missing GraphQLPolicyRegistry var")
	}
	if !strings.Contains(s, "GraphQLPolicyEntry") {
		t.Error("missing GraphQLPolicyEntry type reference")
	}
}

func TestGeneratePolicyRegistryEmpty(t *testing.T) {
	src, err := GeneratePolicyRegistry("policies", nil)
	if err != nil {
		t.Fatalf("GeneratePolicyRegistry: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "PolicyRegistry") {
		t.Error("missing PolicyRegistry var")
	}
}
