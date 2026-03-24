package squeeze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Monorepo(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apps:
  api:
    path: services/api
    migrations:
      - database/migrations
      - ../../shared/migrations
  worker:
    path: services/worker
`
	if err := os.WriteFile(filepath.Join(dir, "pickle.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.IsMonorepo() {
		t.Fatal("expected IsMonorepo() to be true")
	}
	if len(cfg.Apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(cfg.Apps))
	}

	api := cfg.Apps["api"]
	if api.Path != "services/api" {
		t.Errorf("api.Path = %q, want %q", api.Path, "services/api")
	}
	if len(api.Migrations) != 2 {
		t.Fatalf("api.Migrations: got %d, want 2", len(api.Migrations))
	}
	if api.Migrations[1] != "../../shared/migrations" {
		t.Errorf("api.Migrations[1] = %q", api.Migrations[1])
	}

	worker := cfg.Apps["worker"]
	if len(worker.Migrations) != 0 {
		t.Errorf("worker.Migrations: got %d, want 0 (should use default)", len(worker.Migrations))
	}
}

func TestLoadConfig_SingleApp(t *testing.T) {
	dir := t.TempDir()
	yaml := `
squeeze:
  rules:
    no_printf: true
`
	if err := os.WriteFile(filepath.Join(dir, "pickle.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.IsMonorepo() {
		t.Fatal("expected IsMonorepo() to be false for single-app config")
	}
}

func TestLoadConfig_NoFile(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsMonorepo() {
		t.Fatal("expected IsMonorepo() to be false when no file exists")
	}
}
