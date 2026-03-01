package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"User", false},
		{"create_posts_table", false},
		{"RateLimit", false},
		{"admin/User", false},
		{"../../../etc/passwd", true},
		{"../tmp/Evil", true},
		{"admin/../../../etc/passwd", true},
		{"foo\\bar", true},
		{"..", true},
		{".", true},
		{"", true},
	}

	for _, tt := range tests {
		err := sanitizeName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("sanitizeName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestMakeControllerPathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeController("../../../tmp/Evil", dir, "example.com/app")
	if err == nil {
		t.Fatal("expected error for path traversal name, got nil")
	}
}

func TestMakeControllerValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeController("Product", dir, "example.com/app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("app", "http", "controllers", "product_controller.go")
	if relPath != expected {
		t.Errorf("got %q, want %q", relPath, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestWriteScaffoldEscapeCheck(t *testing.T) {
	dir := t.TempDir()
	_, err := writeScaffold(dir, "../outside.go", "package x")
	if err == nil {
		t.Fatal("expected error for path escaping project directory")
	}
}
