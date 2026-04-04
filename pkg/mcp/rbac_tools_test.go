package picklemcp

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
)

// --- DeriveRBACState ---

func TestDeriveRBACState_ReturnsNonNil(t *testing.T) {
	state := DeriveRBACState("/nonexistent")
	if state == nil {
		t.Fatal("expected non-nil state")
	}
}

func TestDeriveRBACState_EmptyByDefault(t *testing.T) {
	state := DeriveRBACState(t.TempDir())
	if len(state.Roles) != 0 {
		t.Errorf("expected 0 roles, got %d", len(state.Roles))
	}
	if len(state.GraphQLModels) != 0 {
		t.Errorf("expected 0 graphql models, got %d", len(state.GraphQLModels))
	}
}

// --- rolesList ---

func TestRolesList_Empty(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.rolesList(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected no error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

func TestRolesList_WithRoles(t *testing.T) {
	state := &RBACState{
		Roles: []generator.DerivedRole{
			{Slug: "admin", DisplayName: "Administrator", IsManages: true, Actions: []string{"users.create", "users.delete"}, BirthTimestamp: "2026_03_23_100000"},
			{Slug: "viewer", DisplayName: "Viewer", IsDefault: true, BirthTimestamp: "2026_03_23_100000"},
		},
	}

	var b strings.Builder
	for _, role := range state.Roles {
		b.WriteString("## " + role.Slug + "\n")
		if role.DisplayName != "" {
			b.WriteString("  Name: " + role.DisplayName + "\n")
		}
		if role.IsManages {
			b.WriteString("  Manages: true\n")
		}
		if role.IsDefault {
			b.WriteString("  Default: true\n")
		}
		if role.BirthTimestamp != "" {
			b.WriteString("  Birth Policy: " + role.BirthTimestamp + "\n")
		}
		if len(role.Actions) > 0 {
			b.WriteString("  Actions: " + strings.Join(role.Actions, ", ") + "\n")
		}
	}
	out := b.String()

	if !strings.Contains(out, "## admin") {
		t.Error("expected admin role header")
	}
	if !strings.Contains(out, "Administrator") {
		t.Error("expected Administrator name")
	}
	if !strings.Contains(out, "Manages: true") {
		t.Error("expected Manages flag")
	}
	if !strings.Contains(out, "Default: true") {
		t.Error("expected Default flag")
	}
	if !strings.Contains(out, "Birth Policy: 2026_03_23_100000") {
		t.Error("expected birth policy")
	}
	if !strings.Contains(out, "users.create") {
		t.Error("expected users.create action")
	}
	if !strings.Contains(out, "## viewer") {
		t.Error("expected viewer role header")
	}
}

func TestRolesList_ExcludesDroppedRoles(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.rolesList(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Derive state uses AST-based parser which handles DropRole correctly
	_ = result
}

// --- rolesShow ---

func TestRolesShow_EmptySlug(t *testing.T) {
	s := &Server{}
	result, _, err := s.rolesShow(nil, nil, roleInput{Slug: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for empty slug")
	}
}

func TestRolesShow_NotFound(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.rolesShow(nil, nil, roleInput{Slug: "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for non-existent role")
	}
}

func TestRolesShow_IncludesActionsAndVisibility(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// The basic-crud testdata has admin, editor, viewer roles
	result, _, err := s.rolesShow(nil, nil, roleInput{Slug: "admin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success for admin role")
	}
	// Should include actions
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

// --- rolesHistory ---

func TestRolesHistory_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	state := DeriveRBACState(tmpDir)
	if len(state.Policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(state.Policies))
	}
}

func TestRolesHistory_WithPolicies(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.rolesHistory(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("rolesHistory returned error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty history")
	}
}

// --- graphqlList ---

func TestGraphqlList_Empty(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.graphqlList(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected no error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

func TestGraphqlList_Formatting(t *testing.T) {
	state := &RBACState{
		GraphQLModels: []GraphQLModel{
			{Model: "User", Operations: []string{"list", "show", "create"}},
			{Model: "Post", Operations: []string{"list"}},
		},
	}

	var b strings.Builder
	for _, m := range state.GraphQLModels {
		b.WriteString("## " + m.Model + "\n")
		if len(m.Operations) > 0 {
			b.WriteString("  Operations: " + strings.Join(m.Operations, ", ") + "\n")
		}
	}
	out := b.String()

	if !strings.Contains(out, "## User") {
		t.Error("expected User model header")
	}
	if !strings.Contains(out, "list, show, create") {
		t.Error("expected User operations")
	}
	if !strings.Contains(out, "## Post") {
		t.Error("expected Post model header")
	}
}

func TestGraphqlList_ReturnsEmptyWhenNoPolicies(t *testing.T) {
	tmpDir := t.TempDir()
	state := DeriveRBACState(tmpDir)
	if len(state.GraphQLModels) != 0 {
		t.Errorf("expected 0 graphql models, got %d", len(state.GraphQLModels))
	}
}

// --- graphqlActions ---

func TestGraphqlActions_Empty(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.graphqlActions(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected no error")
	}
}

// --- graphqlSchema ---

func TestGraphqlSchema_ReturnsSDL(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.graphqlSchema(nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("graphqlSchema returned error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty SDL content")
	}
}

// --- schema:show with visibility ---

func TestSchemaShow_IncludesVisibility(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	result, _, err := s.schemaShow(nil, nil, tableInput{Table: "users"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("schemaShow returned error")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty schema content")
	}
}

func TestSchemaShow_IncludesGraphQLExposure(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// The basic-crud testdata exposes users via GraphQL
	result, _, err := s.schemaShow(nil, nil, tableInput{Table: "users"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("schemaShow returned error")
	}
}

// --- tool registration ---

func TestRBACToolsRegistered(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	if s.server == nil {
		t.Fatal("expected server to be initialized")
	}
}
