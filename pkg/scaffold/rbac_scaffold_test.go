package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- MakePolicy ---

func TestMakePolicyValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakePolicy("access_control", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(relPath, filepath.Join("database", "policies")) {
		t.Errorf("expected path in database/policies, got %q", relPath)
	}
	if !strings.HasSuffix(relPath, "_access_control.go") {
		t.Errorf("expected _access_control.go suffix, got %q", relPath)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakePolicyContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakePolicy("user_roles", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "package policies") {
		t.Error("expected package policies")
	}
	if !strings.Contains(s, "Policy") {
		t.Error("expected embedded Policy struct")
	}
	if !strings.Contains(s, "func (p *") {
		t.Error("expected Up/Down methods")
	}
}

func TestMakePolicyPascalName(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakePolicy("AdminAccess", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "admin_access") {
		t.Errorf("expected snake_case filename, got %q", relPath)
	}
}

func TestMakePolicyInvalidName(t *testing.T) {
	dir := t.TempDir()
	_, err := MakePolicy("../evil", dir)
	if err == nil {
		t.Fatal("expected error for path traversal name")
	}
}

func TestMakePolicyDuplicate(t *testing.T) {
	dir := t.TempDir()
	_, err := MakePolicy("test_policy", dir)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	// Second call with same name at same second should fail
	_, err = MakePolicy("test_policy", dir)
	if err == nil {
		t.Fatal("expected error for duplicate policy")
	}
}

// --- MakeAction ---

func TestMakeActionValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeAction("Post/publish", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("app", "actions", "post", "publish.go")
	if relPath != expected {
		t.Errorf("got %q, want %q", relPath, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeActionContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeAction("Post/publish", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "package actions") {
		t.Error("expected package actions")
	}
	if !strings.Contains(s, "PublishAction") {
		t.Error("expected PublishAction struct")
	}
	if !strings.Contains(s, "Authorize") {
		t.Error("expected Authorize method")
	}
	if !strings.Contains(s, "Handle") {
		t.Error("expected Handle method")
	}
}

func TestMakeActionInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeAction("noslash", dir)
	if err == nil {
		t.Fatal("expected error for missing slash")
	}
}

func TestMakeActionEmptyParts(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeAction("/publish", dir)
	if err == nil {
		t.Fatal("expected error for empty model")
	}
	_, err = MakeAction("Post/", dir)
	if err == nil {
		t.Fatal("expected error for empty action")
	}
}

func TestMakeActionDuplicate(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeAction("Post/publish", dir)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = MakeAction("Post/publish", dir)
	if err == nil {
		t.Fatal("expected error for duplicate action")
	}
}

// --- MakeScope ---

func TestMakeScopeValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeScope("Post/published", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("app", "scopes", "post", "published.go")
	if relPath != expected {
		t.Errorf("got %q, want %q", relPath, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeScopeContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeScope("Post/published", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "package scopes") {
		t.Error("expected package scopes")
	}
	if !strings.Contains(s, "Published") {
		t.Error("expected Published function")
	}
}

func TestMakeScopeInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeScope("noslash", dir)
	if err == nil {
		t.Fatal("expected error for missing slash")
	}
}

func TestMakeScopeDuplicate(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeScope("Post/published", dir)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = MakeScope("Post/published", dir)
	if err == nil {
		t.Fatal("expected error for duplicate scope")
	}
}

// --- MakeGraphQLPolicy ---

func TestMakeGraphQLPolicyValid(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeGraphQLPolicy("user_api", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(relPath, filepath.Join("database", "policies", "graphql")) {
		t.Errorf("expected path in database/policies/graphql, got %q", relPath)
	}
	if !strings.HasSuffix(relPath, "_user_api.go") {
		t.Errorf("expected _user_api.go suffix, got %q", relPath)
	}
	if _, err := os.Stat(filepath.Join(dir, relPath)); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestMakeGraphQLPolicyContent(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeGraphQLPolicy("user_api", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := os.ReadFile(filepath.Join(dir, relPath))
	s := string(content)
	if !strings.Contains(s, "package graphql") {
		t.Error("expected package graphql")
	}
	if !strings.Contains(s, "GraphQLPolicy") {
		t.Error("expected embedded GraphQLPolicy struct")
	}
	if !strings.Contains(s, "func (p *") {
		t.Error("expected Up/Down methods")
	}
}

func TestMakeGraphQLPolicyPascalName(t *testing.T) {
	dir := t.TempDir()
	relPath, err := MakeGraphQLPolicy("PublicApi", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(relPath, "public_api") {
		t.Errorf("expected snake_case filename, got %q", relPath)
	}
}

func TestMakeGraphQLPolicyInvalidName(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeGraphQLPolicy("../evil", dir)
	if err == nil {
		t.Fatal("expected error for path traversal name")
	}
}

func TestMakeGraphQLPolicyDuplicate(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeGraphQLPolicy("test_gql", dir)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = MakeGraphQLPolicy("test_gql", dir)
	if err == nil {
		t.Fatal("expected error for duplicate graphql policy")
	}
}

// --- splitModelSlash ---

func TestSplitModelSlash_Valid(t *testing.T) {
	model, action, err := splitModelSlash("Post/publish", "action")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if model != "Post" {
		t.Errorf("expected model Post, got %q", model)
	}
	if action != "publish" {
		t.Errorf("expected action publish, got %q", action)
	}
}

func TestSplitModelSlash_NoSlash(t *testing.T) {
	_, _, err := splitModelSlash("noslash", "action")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSplitModelSlash_EmptyModel(t *testing.T) {
	_, _, err := splitModelSlash("/publish", "action")
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestSplitModelSlash_EmptyAction(t *testing.T) {
	_, _, err := splitModelSlash("Post/", "action")
	if err == nil {
		t.Fatal("expected error for empty action")
	}
}

// --- template content ---

func TestTmplMakePolicyContent(t *testing.T) {
	out := tmplMakePolicy("TestPolicy_2026_01_01_000000")
	if !strings.Contains(out, "package policies") {
		t.Error("expected package policies")
	}
	if !strings.Contains(out, "TestPolicy_2026_01_01_000000") {
		t.Error("expected struct name")
	}
	if !strings.Contains(out, "func (p *TestPolicy_2026_01_01_000000) Up()") {
		t.Error("expected Up method")
	}
	if !strings.Contains(out, "func (p *TestPolicy_2026_01_01_000000) Down()") {
		t.Error("expected Down method")
	}
}

func TestTmplMakeActionContent(t *testing.T) {
	out := tmplMakeAction("PublishAction", "Post")
	if !strings.Contains(out, "package actions") {
		t.Error("expected package actions")
	}
	if !strings.Contains(out, "PublishAction") {
		t.Error("expected PublishAction struct")
	}
	if !strings.Contains(out, "Authorize") {
		t.Error("expected Authorize method")
	}
	if !strings.Contains(out, "Handle") {
		t.Error("expected Handle method")
	}
}

func TestTmplMakeScopeContent(t *testing.T) {
	out := tmplMakeScope("Published", "Post")
	if !strings.Contains(out, "package scopes") {
		t.Error("expected package scopes")
	}
	if !strings.Contains(out, "func Published()") {
		t.Error("expected Published function")
	}
}

func TestTmplMakeGraphQLPolicyContent(t *testing.T) {
	out := tmplMakeGraphQLPolicy("TestGQL_2026_01_01_000000")
	if !strings.Contains(out, "package graphql") {
		t.Error("expected package graphql")
	}
	if !strings.Contains(out, "TestGQL_2026_01_01_000000") {
		t.Error("expected struct name")
	}
	if !strings.Contains(out, "func (p *TestGQL_2026_01_01_000000) Up()") {
		t.Error("expected Up method")
	}
}
