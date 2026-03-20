package cooked

import "testing"

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
