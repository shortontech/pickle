package cooked

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resetEnv resets the package-level env state so tests don't interfere.
func resetEnv() {
	envOnce = sync.Once{}
	envMap = nil
}

func TestEnvFallback(t *testing.T) {
	resetEnv()
	// No .env file, no env var set → returns fallback
	val := Env("PICKLE_TEST_NONEXISTENT_KEY_XYZ", "default-value")
	if val != "default-value" {
		t.Errorf("Env fallback = %q, want default-value", val)
	}
}

func TestEnvFromOS(t *testing.T) {
	resetEnv()
	os.Setenv("PICKLE_TEST_OS_KEY", "from-os")
	defer os.Unsetenv("PICKLE_TEST_OS_KEY")

	val := Env("PICKLE_TEST_OS_KEY", "fallback")
	if val != "from-os" {
		t.Errorf("Env from OS = %q, want from-os", val)
	}
}

func TestEnvFromDotEnvFile(t *testing.T) {
	resetEnv()

	// Write a temp .env file in a temp dir, then change to that dir
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "# comment\n\nPICKLE_TEST_FILE_KEY=file-value\nQUOTED_KEY=\"quoted value\"\nSINGLE_QUOTED='single'\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)

	val := Env("PICKLE_TEST_FILE_KEY", "fallback")
	if val != "file-value" {
		t.Errorf("Env from .env = %q, want file-value", val)
	}
	val2 := Env("QUOTED_KEY", "fallback")
	if val2 != "quoted value" {
		t.Errorf("Env quoted = %q, want 'quoted value'", val2)
	}
	val3 := Env("SINGLE_QUOTED", "fallback")
	if val3 != "single" {
		t.Errorf("Env single-quoted = %q, want 'single'", val3)
	}
}

func TestEnvOSTakesPrecedenceOverDotEnv(t *testing.T) {
	resetEnv()

	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	os.WriteFile(envFile, []byte("PICKLE_PRECEDENCE=from-file\n"), 0644)

	os.Setenv("PICKLE_PRECEDENCE", "from-os")
	defer os.Unsetenv("PICKLE_PRECEDENCE")

	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	os.Chdir(dir)

	val := Env("PICKLE_PRECEDENCE", "fallback")
	if val != "from-os" {
		t.Errorf("OS env should take precedence over .env, got %q", val)
	}
}

// --- ConnectionConfig.DSN ---

func TestDSNPgsql(t *testing.T) {
	c := ConnectionConfig{
		Driver: "pgsql", Host: "localhost", Port: "5432",
		Name: "mydb", User: "admin", Password: "secret",
	}
	dsn := c.DSN()
	if !strings.HasPrefix(dsn, "postgres://") {
		t.Errorf("pgsql DSN = %q, want postgres:// prefix", dsn)
	}
	if !strings.Contains(dsn, "localhost:5432/mydb") {
		t.Errorf("pgsql DSN = %q, missing host/port/name", dsn)
	}
	if !strings.Contains(dsn, "sslmode=disable") {
		t.Errorf("pgsql DSN = %q, missing sslmode=disable", dsn)
	}
}

func TestDSNPgsqlWithOptions(t *testing.T) {
	c := ConnectionConfig{
		Driver: "pgsql", Host: "db.example.com", Port: "5432",
		Name: "prod", User: "user", Password: "pass",
		Options: map[string]string{"sslmode": "require"},
	}
	dsn := c.DSN()
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("pgsql DSN with options = %q, want sslmode=require", dsn)
	}
}

func TestDSNMySQL(t *testing.T) {
	c := ConnectionConfig{
		Driver: "mysql", Host: "localhost", Port: "3306",
		Name: "mydb", User: "root", Password: "pass",
	}
	dsn := c.DSN()
	if !strings.Contains(dsn, "tcp(localhost:3306)/mydb") {
		t.Errorf("mysql DSN = %q, missing tcp host", dsn)
	}
	if !strings.Contains(dsn, "parseTime=true") {
		t.Errorf("mysql DSN = %q, missing parseTime=true", dsn)
	}
}

func TestDSNSQLite(t *testing.T) {
	c := ConnectionConfig{Driver: "sqlite", Name: "/tmp/test.db"}
	dsn := c.DSN()
	if dsn != "/tmp/test.db" {
		t.Errorf("sqlite DSN = %q, want /tmp/test.db", dsn)
	}
}

func TestDSNUnknownDriverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("DSN with unknown driver should panic")
		}
	}()
	c := ConnectionConfig{Driver: "unknown"}
	c.DSN()
}

// --- driverName ---

func TestDriverName(t *testing.T) {
	tests := []struct {
		driver string
		want   string
	}{
		{"pgsql", "pgx"},
		{"mysql", "mysql"},
		{"sqlite", "sqlite3"},
	}
	for _, tt := range tests {
		c := ConnectionConfig{Driver: tt.driver}
		if got := c.driverName(); got != tt.want {
			t.Errorf("driverName(%q) = %q, want %q", tt.driver, got, tt.want)
		}
	}
}

func TestDriverNameUnknownPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("driverName with unknown driver should panic")
		}
	}()
	c := ConnectionConfig{Driver: "oracle"}
	c.driverName()
}
