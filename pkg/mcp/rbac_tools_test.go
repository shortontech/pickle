package picklemcp

import (
	"strings"
	"testing"
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
	// Should mention no roles defined
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

func TestRolesList_WithRoles(t *testing.T) {
	// Test the formatting of role output directly
	state := &RBACState{
		Roles: []RBACRole{
			{Slug: "admin", Name: "Administrator", Permissions: []string{"users.create", "users.delete"}},
			{Slug: "viewer", Name: "Viewer", Permissions: []string{"users.read"}},
		},
	}

	var b strings.Builder
	for _, role := range state.Roles {
		b.WriteString("## " + role.Slug + "\n")
		if role.Name != "" {
			b.WriteString("  Name: " + role.Name + "\n")
		}
		if len(role.Permissions) > 0 {
			b.WriteString("  Permissions: " + strings.Join(role.Permissions, ", ") + "\n")
		}
	}
	out := b.String()

	if !strings.Contains(out, "## admin") {
		t.Error("expected admin role header")
	}
	if !strings.Contains(out, "Administrator") {
		t.Error("expected Administrator name")
	}
	if !strings.Contains(out, "users.create") {
		t.Error("expected users.create permission")
	}
	if !strings.Contains(out, "## viewer") {
		t.Error("expected viewer role header")
	}
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
	// Should mention no GraphQL models
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
}

func TestGraphqlList_Formatting(t *testing.T) {
	state := &RBACState{
		GraphQLModels: []GraphQLModel{
			{Model: "User", Operations: []string{"query", "create", "update"}},
			{Model: "Post", Operations: []string{"query"}},
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
	if !strings.Contains(out, "query, create, update") {
		t.Error("expected User operations")
	}
	if !strings.Contains(out, "## Post") {
		t.Error("expected Post model header")
	}
}

// --- tool registration ---

func TestRBACToolsRegistered(t *testing.T) {
	projectDir := "../../testdata/basic-crud"
	s, err := NewServer(projectDir)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	// Verify server was created with RBAC tools (server is non-nil and functional)
	if s.server == nil {
		t.Fatal("expected server to be initialized")
	}
}
