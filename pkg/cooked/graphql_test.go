package cooked

import (
	"strings"
	"testing"
)

func TestQueryDepth_Flat(t *testing.T) {
	fields := []Field{
		{Name: "users"},
		{Name: "posts"},
	}
	if d := queryDepth(fields); d != 1 {
		t.Errorf("flat query depth = %d, want 1", d)
	}
}

func TestQueryDepth_Nested(t *testing.T) {
	fields := []Field{
		{
			Name: "users",
			Selections: []Field{
				{
					Name: "posts",
					Selections: []Field{
						{Name: "comments"},
					},
				},
			},
		},
	}
	if d := queryDepth(fields); d != 3 {
		t.Errorf("nested query depth = %d, want 3", d)
	}
}

func TestQueryDepth_Empty(t *testing.T) {
	if d := queryDepth(nil); d != 0 {
		t.Errorf("empty depth = %d, want 0", d)
	}
}

func TestExtractPageDefault(t *testing.T) {
	page, err := extractPage(nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.First != defaultGraphQLPageSize {
		t.Errorf("default page size = %d, want %d", page.First, defaultGraphQLPageSize)
	}
}

func TestExtractPageRejectsOversizedFirst(t *testing.T) {
	_, err := extractPage(map[string]any{
		"page": map[string]any{"first": "101"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestExtractPageRejectsInvalidValues(t *testing.T) {
	tests := []map[string]any{
		{"page": map[string]any{"first": "0"}},
		{"page": map[string]any{"last": "0"}},
		{"page": map[string]any{"first": "1", "last": "1"}},
		{"page": map[string]any{"after": "bad"}},
	}
	for _, args := range tests {
		if _, err := extractPage(args); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
}

func TestEnforceQueryBudgetRejectsFieldCount(t *testing.T) {
	fields := make([]Field, 0, maxQueryFields+1)
	for i := 0; i < maxQueryFields+1; i++ {
		fields = append(fields, Field{Name: "id"})
	}
	_, err := enforceQueryBudget(&Document{Fields: fields}, defaultQueryBudget())
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "field count") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestEnforceQueryBudgetRejectsAliases(t *testing.T) {
	fields := make([]Field, 0, maxQueryAliases+1)
	for i := 0; i < maxQueryAliases+1; i++ {
		fields = append(fields, Field{Name: "id", Alias: "idAlias"})
	}
	_, err := enforceQueryBudget(&Document{Fields: fields}, defaultQueryBudget())
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "alias count") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestEnforceQueryBudgetRejectsInputNodes(t *testing.T) {
	in := make([]any, maxQueryInputNodes+1)
	for i := range in {
		in[i] = "x"
	}
	_, err := enforceQueryBudget(&Document{Fields: []Field{{Name: "users", Args: map[string]any{"filter": in}}}}, defaultQueryBudget())
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "input node") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestEnforceQueryBudgetRejectsComplexity(t *testing.T) {
	registerGraphQLFieldCosts(map[string]FieldCost{
		"User.posts": {FieldName: "posts", BaseCost: 20, IsList: true, IsRelation: true},
	})
	defer func() { generatedFieldCosts = map[string]FieldCost{} }()

	_, err := enforceQueryBudget(&Document{Fields: []Field{
		{Name: "posts", Args: map[string]any{"page": map[string]any{"first": "100"}}},
	}}, defaultQueryBudget())
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "complexity") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestEnforceQueryBudgetRejectsRelationshipDepth(t *testing.T) {
	registerGraphQLFieldCosts(map[string]FieldCost{
		"A.bs": {FieldName: "bs", BaseCost: 1, IsRelation: true},
		"B.cs": {FieldName: "cs", BaseCost: 1, IsRelation: true},
		"C.ds": {FieldName: "ds", BaseCost: 1, IsRelation: true},
		"D.es": {FieldName: "es", BaseCost: 1, IsRelation: true},
	})
	defer func() { generatedFieldCosts = map[string]FieldCost{} }()

	_, err := enforceQueryBudget(&Document{Fields: []Field{{
		Name: "bs",
		Selections: []Field{{
			Name: "cs",
			Selections: []Field{{
				Name: "ds",
				Selections: []Field{{
					Name: "es",
				}},
			}},
		}},
	}}}, defaultQueryBudget())
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "relationship depth") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestIsIntrospectionField(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"__schema", true},
		{"__type", true},
		{"__typename", true},
		{"users", false},
		{"schema", false},
	}
	for _, tt := range tests {
		if got := isIntrospectionField(tt.name); got != tt.want {
			t.Errorf("isIntrospectionField(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestValidateInput_NoTags(t *testing.T) {
	type input struct {
		Name string
	}
	if err := validateInput(input{Name: "test"}); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateInput_RequiredMissing(t *testing.T) {
	type input struct {
		Name string `validate:"required"`
	}
	err := validateInput(input{Name: ""})
	if err == nil {
		t.Fatal("expected validation error")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Fields) != 1 {
		t.Fatalf("expected 1 field error, got %d", len(ve.Fields))
	}
	if ve.Fields[0].Message != "is required" {
		t.Errorf("message = %q", ve.Fields[0].Message)
	}
}

func TestCamelCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Name", "name"},
		{"email", "email"},
		{"", ""},
		{"A", "a"},
	}
	for _, tt := range tests {
		if got := camelCase(tt.in); got != tt.want {
			t.Errorf("camelCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
