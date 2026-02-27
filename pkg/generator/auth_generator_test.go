package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanAuthDrivers(t *testing.T) {
	// Create temp auth directory with driver subdirs
	tmp := t.TempDir()

	// jwt/ with no driver.go (built-in, needs gen)
	os.MkdirAll(filepath.Join(tmp, "jwt"), 0o755)

	// session/ with no driver.go (built-in, needs gen)
	os.MkdirAll(filepath.Join(tmp, "session"), 0o755)

	// custom/ with driver.go (user-written)
	customDir := filepath.Join(tmp, "custom")
	os.MkdirAll(customDir, 0o755)
	os.WriteFile(filepath.Join(customDir, "driver.go"), []byte("package custom\n"), 0o644)

	drivers, err := ScanAuthDrivers(tmp)
	if err != nil {
		t.Fatalf("ScanAuthDrivers: %v", err)
	}

	if len(drivers) != 3 {
		t.Fatalf("got %d drivers, want 3", len(drivers))
	}

	// Build a map for easier assertions
	byName := map[string]AuthDriverInfo{}
	for _, d := range drivers {
		byName[d.Name] = d
	}

	// jwt: built-in, needs gen
	jwt := byName["jwt"]
	if !jwt.IsBuiltin {
		t.Error("jwt should be built-in")
	}
	if !jwt.NeedsGen {
		t.Error("jwt should need gen (no driver.go)")
	}

	// session: built-in, needs gen
	sess := byName["session"]
	if !sess.IsBuiltin {
		t.Error("session should be built-in")
	}
	if !sess.NeedsGen {
		t.Error("session should need gen (no driver.go)")
	}

	// custom: not built-in
	custom := byName["custom"]
	if custom.IsBuiltin {
		t.Error("custom should not be built-in")
	}
	if custom.NeedsGen {
		t.Error("custom should not need gen (has driver.go)")
	}
}

func TestScanAuthDriversUserOverride(t *testing.T) {
	tmp := t.TempDir()

	// jwt/ with driver.go — user has overridden the built-in
	jwtDir := filepath.Join(tmp, "jwt")
	os.MkdirAll(jwtDir, 0o755)
	os.WriteFile(filepath.Join(jwtDir, "driver.go"), []byte("package jwt\n"), 0o644)

	drivers, err := ScanAuthDrivers(tmp)
	if err != nil {
		t.Fatalf("ScanAuthDrivers: %v", err)
	}

	if len(drivers) != 1 {
		t.Fatalf("got %d drivers, want 1", len(drivers))
	}

	jwt := drivers[0]
	if !jwt.IsBuiltin {
		t.Error("jwt should still be recognized as built-in")
	}
	if jwt.NeedsGen {
		t.Error("jwt should NOT need gen when driver.go exists")
	}
}

func TestGenerateBuiltinDriver(t *testing.T) {
	d := AuthDriverInfo{Name: "jwt", IsBuiltin: true, NeedsGen: true}
	src, err := GenerateBuiltinDriver(d, "myapp/app/http")
	if err != nil {
		t.Fatalf("GenerateBuiltinDriver: %v", err)
	}

	content := string(src)

	// Package should be replaced
	if !strings.Contains(content, "package jwt") {
		t.Error("expected package jwt")
	}

	// Import should be replaced
	if !strings.Contains(content, `pickle "myapp/app/http"`) {
		t.Error("expected cooked import to be replaced with myapp/app/http")
	}

	// Should NOT contain the original cooked import
	if strings.Contains(content, "github.com/shortontech/pickle/pkg/cooked") {
		t.Error("should not contain original cooked import path")
	}
}

func TestGenerateAuthRegistry(t *testing.T) {
	drivers := []AuthDriverInfo{
		{Name: "jwt", IsBuiltin: true, NeedsGen: true},
		{Name: "session", IsBuiltin: true, NeedsGen: true},
	}

	src, err := GenerateAuthRegistry(
		drivers,
		"myapp",
		"myapp/app/http",
	)
	if err != nil {
		t.Fatalf("GenerateAuthRegistry: %v", err)
	}

	content := string(src)

	// Should have correct imports
	if !strings.Contains(content, `pickle "myapp/app/http"`) {
		t.Error("missing pickle import")
	}
	if !strings.Contains(content, `"database/sql"`) {
		t.Error("missing database/sql import")
	}
	if !strings.Contains(content, `jwt "myapp/app/http/auth/jwt"`) {
		t.Error("missing jwt driver import")
	}

	// Should register both drivers
	if !strings.Contains(content, `registry["jwt"]`) {
		t.Error("missing jwt registration")
	}
	if !strings.Contains(content, `registry["session"]`) {
		t.Error("missing session registration")
	}

	// Should have the interface
	if !strings.Contains(content, "type AuthDriver interface") {
		t.Error("missing AuthDriver interface")
	}

	// Should have DefaultAuthMiddleware
	if !strings.Contains(content, "func DefaultAuthMiddleware") {
		t.Error("missing DefaultAuthMiddleware")
	}
}
