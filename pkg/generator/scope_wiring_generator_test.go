package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanScopes(t *testing.T) {
	dir := t.TempDir()

	// Create database/scopes/user/active.go
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "active.go"), []byte(`package user

import "myapp/app/models"

func Active(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	return q.WhereActive(true)
}
`), 0o644)

	// Create database/scopes/user/region.go with extra param
	os.WriteFile(filepath.Join(userDir, "region.go"), []byte(`package user

import "myapp/app/models"

func InRegion(q *models.UserScopeBuilder, region string) *models.UserScopeBuilder {
	return q.WhereRegion(region)
}
`), 0o644)

	result, err := ScanScopes(dir)
	if err != nil {
		t.Fatal(err)
	}

	scopes, ok := result["user"]
	if !ok {
		t.Fatal("expected scopes for 'user'")
	}
	if len(scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(scopes))
	}

	// Sort by name for deterministic test
	if scopes[0].Name > scopes[1].Name {
		scopes[0], scopes[1] = scopes[1], scopes[0]
	}

	if scopes[0].Name != "Active" {
		t.Errorf("expected 'Active', got %q", scopes[0].Name)
	}
	if len(scopes[0].ExtraParams) != 0 {
		t.Errorf("expected 0 extra params for Active, got %d", len(scopes[0].ExtraParams))
	}

	if scopes[1].Name != "InRegion" {
		t.Errorf("expected 'InRegion', got %q", scopes[1].Name)
	}
	if len(scopes[1].ExtraParams) != 1 {
		t.Fatalf("expected 1 extra param for InRegion, got %d", len(scopes[1].ExtraParams))
	}
	if scopes[1].ExtraParams[0].Name != "region" || scopes[1].ExtraParams[0].Type != "string" {
		t.Errorf("unexpected param: %+v", scopes[1].ExtraParams[0])
	}
}

func TestScanScopesEmpty(t *testing.T) {
	dir := t.TempDir()
	result, err := ScanScopes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestScanScopesMissing(t *testing.T) {
	result, err := ScanScopes("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result for missing dir")
	}
}

func TestGenerateScopeWiring(t *testing.T) {
	scopes := []ScopeDef{
		{
			Name:       "Active",
			SourceFile: "database/scopes/user/active.go",
		},
		{
			Name:       "InRegion",
			ExtraParams: []ScopeParam{{Name: "region", Type: "string"}},
			SourceFile: "database/scopes/user/region.go",
		},
	}

	src, err := GenerateScopeWiring("users", scopes, "models", "myapp/database/scopes/user")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// Check Active wrapper
	if !strings.Contains(content, "func (q *UserQuery) Active() *UserQuery") {
		t.Error("expected Active wrapper method")
	}
	if !strings.Contains(content, "// Active — source: database/scopes/user/active.go") {
		t.Error("expected source comment for Active")
	}

	// Check InRegion wrapper with params
	if !strings.Contains(content, "func (q *UserQuery) InRegion(region string) *UserQuery") {
		t.Error("expected InRegion wrapper with region param")
	}
	if !strings.Contains(content, "scopes.InRegion(sb, region)") {
		t.Error("expected scope call with extra param")
	}
}

func TestGenerateScopeWiringEmpty(t *testing.T) {
	src, err := GenerateScopeWiring("users", nil, "models", "myapp/database/scopes/user")
	if err != nil {
		t.Fatal(err)
	}
	if src != nil {
		t.Error("expected nil for empty scopes")
	}
}
