package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGenerateRoleAwareScopes(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSees("support")
	table.String("ssn", 11).NotNull().RoleSees("compliance")
	table.String("phone", 20).Nullable().RoleSees("support").RoleSees("compliance")
	table.String("internal_notes").Nullable() // no visibility
	table.Timestamps()

	blocks := loadScopeBlocks(t)

	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}
	content := string(src)

	// SelectFor should exist
	if !strings.Contains(content, "func (q *UserQuery) SelectFor(role string) *UserQuery") {
		t.Error("expected SelectFor method")
	}

	// Should have switch cases for compliance and support
	if !strings.Contains(content, `case "compliance"`) {
		t.Error("expected compliance case")
	}
	if !strings.Contains(content, `case "support"`) {
		t.Error("expected support case")
	}

	// SelectForRoles should exist
	if !strings.Contains(content, "func (q *UserQuery) SelectForRoles(roles []string) *UserQuery") {
		t.Error("expected SelectForRoles method")
	}

	// SelectForOwner should exist
	if !strings.Contains(content, "func (q *UserQuery) SelectForOwner(roles []string) *UserQuery") {
		t.Error("expected SelectForOwner method")
	}

	// Default case should return public columns
	if !strings.Contains(content, `"id"`) && !strings.Contains(content, `"name"`) {
		t.Error("expected public columns in default case")
	}
}

func TestRoleAwareScopesNoRoles(t *testing.T) {
	table := &schema.Table{Name: "posts"}
	table.UUID("id").PrimaryKey().Public()
	table.String("title", 255).NotNull().Public()
	table.Text("body").NotNull()
	table.Timestamps()

	blocks := loadScopeBlocks(t)

	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// SelectFor should still exist with empty switch (just default case)
	if !strings.Contains(content, "func (q *PostQuery) SelectFor(role string) *PostQuery") {
		t.Error("expected SelectFor even with no role annotations")
	}
}

func TestMergeColSets(t *testing.T) {
	result := mergeColSets([]string{"a", "b"}, []string{"b", "c"})
	if len(result) != 3 {
		t.Fatalf("expected 3 cols, got %d: %v", len(result), result)
	}
	expected := map[string]bool{"a": true, "b": true, "c": true}
	for _, c := range result {
		if !expected[c] {
			t.Errorf("unexpected col %q", c)
		}
	}
}
