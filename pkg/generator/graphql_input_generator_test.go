package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestBuildRequestValidationMap(t *testing.T) {
	requests := []RequestDef{
		{
			Name: "CreateUserRequest",
			Fields: []RequestField{
				{Name: "Name", Type: "string", Validate: "required,min=1,max=255"},
				{Name: "Email", Type: "string", Validate: "required,email"},
			},
		},
	}

	m := BuildRequestValidationMap(requests)
	if m["CreateUserInput.Name"] != "required,min=1,max=255" {
		t.Errorf("got %q", m["CreateUserInput.Name"])
	}
	if m["CreateUserInput.Email"] != "required,email" {
		t.Errorf("got %q", m["CreateUserInput.Email"])
	}
}

func TestExtractEnums(t *testing.T) {
	requests := []RequestDef{
		{
			Name: "UpdatePostRequest",
			Fields: []RequestField{
				{Name: "Status", Type: "*string", Validate: "omitempty,oneof=draft published archived"},
			},
		},
	}

	enums := ExtractEnums(requests)
	if len(enums) != 1 {
		t.Fatalf("expected 1 enum, got %d", len(enums))
	}
	if enums[0].Name != "PostStatus" {
		t.Errorf("enum name = %q, want PostStatus", enums[0].Name)
	}
	if len(enums[0].Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(enums[0].Values))
	}
	if enums[0].Values[0] != "DRAFT" || enums[0].Values[1] != "PUBLISHED" || enums[0].Values[2] != "ARCHIVED" {
		t.Errorf("values = %v", enums[0].Values)
	}
}

func TestExtractEnums_Deduplication(t *testing.T) {
	requests := []RequestDef{
		{
			Name: "CreatePostRequest",
			Fields: []RequestField{
				{Name: "Status", Type: "string", Validate: "required,oneof=draft published"},
			},
		},
		{
			Name: "UpdatePostRequest",
			Fields: []RequestField{
				{Name: "Status", Type: "*string", Validate: "omitempty,oneof=draft published archived"},
			},
		},
	}

	enums := ExtractEnums(requests)
	if len(enums) != 1 {
		t.Errorf("expected 1 deduplicated enum, got %d", len(enums))
	}
}

func TestEnumFieldMap(t *testing.T) {
	requests := []RequestDef{
		{
			Name: "UpdatePostRequest",
			Fields: []RequestField{
				{Name: "Status", Type: "*string", Validate: "omitempty,oneof=draft published archived"},
			},
		},
	}

	m := EnumFieldMap(requests)
	if m["UpdatePostInput.Status"] != "PostStatus" {
		t.Errorf("got %q", m["UpdatePostInput.Status"])
	}
}

func TestGraphQLTypesWithValidation(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String},
				{Name: "email", Type: schema.String},
			},
		},
	}
	requests := []RequestDef{
		{
			Name: "CreateUserRequest",
			Fields: []RequestField{
				{Name: "Name", Type: "string", Validate: "required,min=1,max=255"},
				{Name: "Email", Type: "string", Validate: "required,email"},
			},
		},
	}

	src, err := GenerateGraphQLTypes(tables, requests, "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, `validate:"required,min=1,max=255"`) {
		t.Error("CreateUserInput.Name should have validate tag")
	}
	if !strings.Contains(s, `validate:"required,email"`) {
		t.Error("CreateUserInput.Email should have validate tag")
	}
}

func TestGraphQLSchemaWithEnums(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "title", Type: schema.String},
				{Name: "status", Type: schema.String},
			},
		},
	}
	requests := []RequestDef{
		{
			Name: "UpdatePostRequest",
			Fields: []RequestField{
				{Name: "Status", Type: "*string", Validate: "omitempty,oneof=draft published archived"},
			},
		},
	}

	src, err := GenerateGraphQLSchema(tables, nil, requests, "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "enum PostStatus") {
		t.Error("should generate PostStatus enum")
	}
	if !strings.Contains(s, "DRAFT") {
		t.Error("enum should contain DRAFT")
	}
	// The UpdatePostInput should use PostStatus instead of String
	if !strings.Contains(s, "status: PostStatus") {
		t.Error("UpdatePostInput.status should use PostStatus enum type")
	}
}
