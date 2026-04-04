package generator

import (
	"fmt"
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

func TestScanScopesRejectsTerminalFirst(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "bad.go"), []byte(`package user

import "myapp/app/models"

func Bad(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	q.First()
	return q
}
`), 0o644)

	_, err := ScanScopes(dir)
	if err == nil {
		t.Fatal("expected error for scope calling First()")
	}
	if !strings.Contains(err.Error(), "terminal method First()") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestScanScopesRejectsTerminalDelete(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "bad.go"), []byte(`package user

import "myapp/app/models"

func Bad(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	q.Delete(nil)
	return q
}
`), 0o644)

	_, err := ScanScopes(dir)
	if err == nil {
		t.Fatal("expected error for scope calling Delete()")
	}
	if !strings.Contains(err.Error(), "terminal method Delete()") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestScanScopesRejectsTerminalCreate(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "bad.go"), []byte(`package user

import "myapp/app/models"

func Bad(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	q.Create(nil)
	return q
}
`), 0o644)

	_, err := ScanScopes(dir)
	if err == nil {
		t.Fatal("expected error for scope calling Create()")
	}
	if !strings.Contains(err.Error(), "terminal method Create()") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestScanScopesAllowsFilterMethods(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "good.go"), []byte(`package user

import "myapp/app/models"

func Active(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	return q.WhereActive(true)
}
`), 0o644)

	result, err := ScanScopes(dir)
	if err != nil {
		t.Fatalf("unexpected error for valid scope: %v", err)
	}
	if len(result["user"]) != 1 {
		t.Errorf("expected 1 scope, got %d", len(result["user"]))
	}
}

func TestScanScopesComposition(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)

	// A scope that calls another scope function — this is plain function composition
	os.WriteFile(filepath.Join(userDir, "active.go"), []byte(`package user

import "myapp/app/models"

func Active(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	return q.WhereActive(true)
}
`), 0o644)

	os.WriteFile(filepath.Join(userDir, "active_in_region.go"), []byte(`package user

import "myapp/app/models"

func ActiveInRegion(q *models.UserScopeBuilder, region string) *models.UserScopeBuilder {
	return InRegion(Active(q), region)
}

func InRegion(q *models.UserScopeBuilder, region string) *models.UserScopeBuilder {
	return q.WhereRegion(region)
}
`), 0o644)

	result, err := ScanScopes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scopes := result["user"]
	if len(scopes) != 3 {
		t.Fatalf("expected 3 scopes (Active, ActiveInRegion, InRegion), got %d", len(scopes))
	}

	// Verify ActiveInRegion was parsed with its extra param
	found := false
	for _, s := range scopes {
		if s.Name == "ActiveInRegion" {
			found = true
			if len(s.ExtraParams) != 1 || s.ExtraParams[0].Name != "region" {
				t.Errorf("ActiveInRegion should have 1 extra param 'region', got %+v", s.ExtraParams)
			}
		}
	}
	if !found {
		t.Error("expected ActiveInRegion scope to be found")
	}
}

func TestScanScopesRejectsBadSignature(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)

	// First param is not a pointer — should be rejected
	os.WriteFile(filepath.Join(userDir, "bad.go"), []byte(`package user

func Bad(q string) string {
	return q
}
`), 0o644)

	_, err := ScanScopes(dir)
	if err == nil {
		t.Fatal("expected error for scope with non-pointer first param")
	}
	if !strings.Contains(err.Error(), "pointer") {
		t.Errorf("expected pointer error, got: %v", err)
	}
}

func TestGenerateScopeWiringMultipleModels(t *testing.T) {
	dir := t.TempDir()

	// Create scopes for two different models
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "active.go"), []byte(`package user

import "myapp/app/models"

func Active(q *models.UserScopeBuilder) *models.UserScopeBuilder {
	return q.WhereActive(true)
}
`), 0o644)

	postDir := filepath.Join(dir, "post")
	os.MkdirAll(postDir, 0o755)
	os.WriteFile(filepath.Join(postDir, "published.go"), []byte(`package post

import "myapp/app/models"

func Published(q *models.PostScopeBuilder) *models.PostScopeBuilder {
	return q.WherePublished(true)
}
`), 0o644)

	result, err := ScanScopes(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 model scope sets, got %d", len(result))
	}
	if len(result["user"]) != 1 {
		t.Errorf("expected 1 user scope, got %d", len(result["user"]))
	}
	if len(result["post"]) != 1 {
		t.Errorf("expected 1 post scope, got %d", len(result["post"]))
	}
}

func TestGenerateScopeWiringChaining(t *testing.T) {
	// Verify that generated wrappers return *XxxQuery to enable chaining
	scopes := []ScopeDef{
		{Name: "Active", SourceFile: "database/scopes/user/active.go"},
		{Name: "InRegion", ExtraParams: []ScopeParam{{Name: "region", Type: "string"}}, SourceFile: "database/scopes/user/region.go"},
		{Name: "RecentlyCreated", SourceFile: "database/scopes/user/recent.go"},
	}

	src, err := GenerateScopeWiring("users", scopes, "models", "myapp/database/scopes/user")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// All three wrappers should return *UserQuery for chaining
	for _, name := range []string{"Active", "InRegion", "RecentlyCreated"} {
		if !strings.Contains(content, fmt.Sprintf("func (q *UserQuery) %s(", name)) {
			t.Errorf("expected wrapper for %s", name)
		}
	}

	// Verify return type enables chaining
	if count := strings.Count(content, "*UserQuery {"); count != 3 {
		t.Errorf("expected 3 methods returning *UserQuery, got %d", count)
	}

	// Verify source comments for all three
	if !strings.Contains(content, "// RecentlyCreated — source: database/scopes/user/recent.go") {
		t.Error("expected source comment for RecentlyCreated")
	}
}
