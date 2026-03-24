package generator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveImportPath_InsideModule(t *testing.T) {
	appDir := "/project/services/api"
	modulePath := "myapp/api"
	absDir := "/project/services/api/database/migrations"

	got := ResolveImportPath(appDir, modulePath, absDir)
	want := "myapp/api/database/migrations"
	if got != want {
		t.Errorf("ResolveImportPath = %q, want %q", got, want)
	}
}

func TestResolveImportPath_ExternalWithGoMod(t *testing.T) {
	// Set up a temp directory with a go.mod
	dir := t.TempDir()
	sharedDir := filepath.Join(dir, "shared", "migrations")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := "module shared\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "shared", "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	appDir := filepath.Join(dir, "services", "api")
	got := ResolveImportPath(appDir, "myapp/api", sharedDir)
	want := "shared/migrations"
	if got != want {
		t.Errorf("ResolveImportPath = %q, want %q", got, want)
	}
}

func TestFindModuleRoot(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	root, modPath := findModuleRoot(sub)
	if root != dir {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if modPath != "example.com/foo" {
		t.Errorf("modPath = %q, want %q", modPath, "example.com/foo")
	}
}
