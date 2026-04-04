package generator

import (
	"bytes"
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

func TestScopeBuilderExpandedMethods(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSees("support")
	table.Integer("age").NotNull()
	table.Timestamp("created_at").NotNull()
	table.Timestamps()

	blocks := loadScopeBlocks(t)

	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// ScopeBuilder type should exist
	if !strings.Contains(content, "type UserScopeBuilder struct") {
		t.Error("expected UserScopeBuilder type")
	}

	// Basic Where (equality)
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereName(") {
		t.Error("expected WhereName on ScopeBuilder")
	}

	// WhereNot
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereNameNot(") {
		t.Error("expected WhereNameNot on ScopeBuilder")
	}

	// WhereIn
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereNameIn(") {
		t.Error("expected WhereNameIn on ScopeBuilder")
	}

	// WhereNotIn
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereNameNotIn(") {
		t.Error("expected WhereNameNotIn on ScopeBuilder")
	}

	// String-specific: Like
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereNameLike(") {
		t.Error("expected WhereNameLike on ScopeBuilder")
	}

	// String-specific: NotLike
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereNameNotLike(") {
		t.Error("expected WhereNameNotLike on ScopeBuilder")
	}

	// Numeric: GT, GTE, LT, LTE
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereAgeGT(") {
		t.Error("expected WhereAgeGT on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereAgeGTE(") {
		t.Error("expected WhereAgeGTE on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereAgeLT(") {
		t.Error("expected WhereAgeLT on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereAgeLTE(") {
		t.Error("expected WhereAgeLTE on ScopeBuilder")
	}

	// Timestamp: Before, After, Between
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereCreatedAtBefore(") {
		t.Error("expected WhereCreatedAtBefore on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereCreatedAtAfter(") {
		t.Error("expected WhereCreatedAtAfter on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) WhereCreatedAtBetween(") {
		t.Error("expected WhereCreatedAtBetween on ScopeBuilder")
	}

	// OrderBy typed methods
	if !strings.Contains(content, "func (sb *UserScopeBuilder) OrderByName(") {
		t.Error("expected OrderByName on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) OrderByAge(") {
		t.Error("expected OrderByAge on ScopeBuilder")
	}

	// Visibility methods
	if !strings.Contains(content, "func (sb *UserScopeBuilder) SelectPublic()") {
		t.Error("expected SelectPublic on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) SelectOwner()") {
		t.Error("expected SelectOwner on ScopeBuilder")
	}
	if !strings.Contains(content, "func (sb *UserScopeBuilder) SelectAll()") {
		t.Error("expected SelectAll on ScopeBuilder")
	}

	// ScopeBuilder should NOT have terminal methods
	if strings.Contains(content, "func (sb *UserScopeBuilder) First(") {
		t.Error("ScopeBuilder must not have First()")
	}
	if strings.Contains(content, "func (sb *UserScopeBuilder) All(") {
		t.Error("ScopeBuilder must not have All()")
	}
	if strings.Contains(content, "func (sb *UserScopeBuilder) Create(") {
		t.Error("ScopeBuilder must not have Create()")
	}
	if strings.Contains(content, "func (sb *UserScopeBuilder) Delete(") {
		t.Error("ScopeBuilder must not have Delete()")
	}
	if strings.Contains(content, "func (sb *UserScopeBuilder) Update(") {
		t.Error("ScopeBuilder must not have Update()")
	}
	if strings.Contains(content, "func (sb *UserScopeBuilder) Count(") {
		t.Error("ScopeBuilder must not have Count()")
	}
}

func TestSelectForManagesReturnsAllColumns(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSees("support")
	table.String("ssn", 11).NotNull().RoleSees("compliance")
	table.Timestamps()

	blocks := loadScopeBlocks(t)

	// Use the WithBirth variant to pass manages roles
	var b bytes.Buffer
	b.WriteString("package models\n\n")
	managesRoles := map[string]bool{"administrator": true}
	generateRoleAwareScopesWithBirth(&b, table, "UserQuery", []string{"id", "name"}, []string{"id", "name"}, nil, managesRoles)

	content := b.String()
	_ = blocks

	// Administrator case should set selectedCols = nil (all columns)
	if !strings.Contains(content, `case "administrator"`) {
		t.Error("expected administrator case")
	}
	if !strings.Contains(content, "q.selectedCols = nil") {
		t.Error("expected nil selectedCols for manages role")
	}
}

func TestSelectForUnknownRoleReturnsPublic(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSees("support")
	table.Timestamps()

	blocks := loadScopeBlocks(t)
	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// Default case should have public columns
	if !strings.Contains(content, `q.selectedCols = []string{"id", "name"}`) {
		t.Error("expected default case to select public columns only")
	}
}

func TestBirthTimestampExcludesOldAnnotations(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	// This annotation was added in migration 001, but role was born in 003
	table.String("email", 255).NotNull().RoleSeesFrom("support", "001")
	// This annotation was added in migration 005, after role birth
	table.String("phone", 20).Nullable().RoleSeesFrom("support", "005")

	var b bytes.Buffer
	b.WriteString("package models\n\n")
	roleBirths := map[string]string{"support": "003"}
	generateRoleAwareScopesWithBirth(&b, table, "UserQuery", []string{"id", "name"}, []string{"id", "name"}, roleBirths, nil)

	content := b.String()

	// Support case should include phone (005 >= 003) but NOT email (001 < 003)
	if !strings.Contains(content, `"phone"`) {
		t.Error("expected phone (post-birth annotation) in support case")
	}
	// email should not appear in the support case column list
	if strings.Contains(content, `"email"`) {
		t.Error("email (pre-birth annotation) should be excluded from support case")
	}
}

func TestBirthTimestampIncludesPostBirthAnnotations(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSeesFrom("support", "005")

	var b bytes.Buffer
	b.WriteString("package models\n\n")
	roleBirths := map[string]string{"support": "003"}
	generateRoleAwareScopesWithBirth(&b, table, "UserQuery", []string{"id", "name"}, []string{"id", "name"}, roleBirths, nil)

	content := b.String()

	if !strings.Contains(content, `"email"`) {
		t.Error("expected email (post-birth annotation) to be included")
	}
}

func TestBirthTimestampDropAndRecreate(t *testing.T) {
	// Role "support" was created at 001, dropped, recreated at 005.
	// Annotations from 002 (pre-recreation) should be excluded.
	// Annotations from 006 (post-recreation) should be included.
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("old_field", 255).NotNull().RoleSeesFrom("support", "002")
	table.String("new_field", 255).NotNull().RoleSeesFrom("support", "006")

	var b bytes.Buffer
	b.WriteString("package models\n\n")
	roleBirths := map[string]string{"support": "005"} // recreated at 005
	generateRoleAwareScopesWithBirth(&b, table, "UserQuery", []string{"id", "name"}, []string{"id", "name"}, roleBirths, nil)

	content := b.String()

	if strings.Contains(content, `"old_field"`) {
		t.Error("old_field (pre-recreation annotation) should be excluded")
	}
	if !strings.Contains(content, `"new_field"`) {
		t.Error("new_field (post-recreation annotation) should be included")
	}
}

func TestSelectForRolesMultipleRolesUnion(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSees("support")
	table.String("ssn", 11).NotNull().RoleSees("compliance")
	table.Timestamps()

	blocks := loadScopeBlocks(t)
	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// SelectForRoles should contain both role cases
	if !strings.Contains(content, "func (q *UserQuery) SelectForRoles(roles []string) *UserQuery") {
		t.Error("expected SelectForRoles method")
	}
	// Both roles should have their column lists
	if !strings.Contains(content, `"email"`) {
		t.Error("expected email for support role")
	}
	if !strings.Contains(content, `"ssn"`) {
		t.Error("expected ssn for compliance role")
	}
}

func TestSelectForOwnerAddsOwnerColumns(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("secret", 255).NotNull().OwnerSees()
	table.String("email", 255).NotNull().RoleSees("support")
	table.Timestamps()

	blocks := loadScopeBlocks(t)
	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// SelectForOwner should merge owner columns
	if !strings.Contains(content, "func (q *UserQuery) SelectForOwner(roles []string) *UserQuery") {
		t.Error("expected SelectForOwner method")
	}
	if !strings.Contains(content, `"secret"`) {
		t.Error("expected secret in owner columns")
	}
}

func TestSpec002AliasesStillWork(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("secret", 255).NotNull().OwnerSees()
	table.Timestamps()

	blocks := loadScopeBlocks(t)
	src, err := GenerateQueryScopes(table, blocks, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "func (q *UserQuery) SelectPublic() *UserQuery") {
		t.Error("expected SelectPublic alias")
	}
	if !strings.Contains(content, "func (q *UserQuery) SelectOwner() *UserQuery") {
		t.Error("expected SelectOwner alias")
	}
	if !strings.Contains(content, "func (q *UserQuery) SelectAll() *UserQuery") {
		t.Error("expected SelectAll alias")
	}
}

func TestForRolesSerializationOmitsCorrectFields(t *testing.T) {
	table := &schema.Table{Name: "users"}
	table.UUID("id").PrimaryKey().Public()
	table.String("name", 255).NotNull().Public()
	table.String("email", 255).NotNull().RoleSees("support")
	table.String("ssn", 11).NotNull().RoleSees("compliance")
	table.String("internal_notes").Nullable() // no visibility — should never appear

	src, err := GenerateModel(table, "models")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	// ForRoles method should exist
	if !strings.Contains(content, "func (u *User) ForRoles(roles []string) map[string]any") {
		t.Error("expected ForRoles method on model")
	}

	// ForOwner method should exist
	if !strings.Contains(content, "func (u *User) ForOwner(roles []string) map[string]any") {
		t.Error("expected ForOwner method on model")
	}

	// Public columns should always be in ForRoles output
	if !strings.Contains(content, `out["id"]`) {
		t.Error("expected id in ForRoles output")
	}
	if !strings.Contains(content, `out["name"]`) {
		t.Error("expected name in ForRoles output")
	}

	// Role-gated columns should be conditional
	if !strings.Contains(content, `roleSet["support"]`) {
		t.Error("expected support role check for email")
	}
	if !strings.Contains(content, `roleSet["compliance"]`) {
		t.Error("expected compliance role check for ssn")
	}

	// internal_notes has no visibility — should not appear in ForRoles output map
	if strings.Contains(content, `out["internal_notes"]`) {
		t.Error("internal_notes should not appear in ForRoles output")
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
