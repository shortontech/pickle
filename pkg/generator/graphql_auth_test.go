package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGraphQLSchemaDirectives(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String, IsPublic: true},
				{Name: "email", Type: schema.String, IsOwnerSees: true},
				{Name: "role", Type: schema.String},
			},
		},
	}

	src, err := GenerateGraphQLSchema(tables, nil, nil, "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// Directive declarations
	if !strings.Contains(s, `directive @public on FIELD_DEFINITION`) {
		t.Error("missing @public directive declaration")
	}
	if !strings.Contains(s, `directive @auth on FIELD_DEFINITION`) {
		t.Error("missing @auth directive declaration")
	}
	if !strings.Contains(s, `directive @ownerOnly on FIELD_DEFINITION`) {
		t.Error("missing @ownerOnly directive declaration")
	}
	if !strings.Contains(s, `directive @requireRole(roles: [String!]!) on FIELD_DEFINITION`) {
		t.Error("missing @requireRole directive declaration")
	}

	// Field-level directives
	if !strings.Contains(s, "id: ID! @public") {
		t.Error("PK should have @public")
	}
	if !strings.Contains(s, "name: String! @public") {
		t.Error("Public column should have @public")
	}
	if !strings.Contains(s, "email: String! @ownerOnly") {
		t.Error("OwnerSees column should have @ownerOnly")
	}
	if !strings.Contains(s, "role: String! @auth") {
		t.Error("unannotated column should have @auth")
	}
}

func TestGraphQLMutationDirectives(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "title", Type: schema.String},
			},
		},
	}

	src, err := GenerateGraphQLSchema(tables, nil, nil, "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "createPost(input: CreatePostInput!): Post! @auth") {
		t.Error("mutations should have @auth")
	}
	if !strings.Contains(s, "deletePost(id: ID!): Boolean! @auth") {
		t.Error("delete mutations should have @auth")
	}
}

func TestGraphQLRelationshipDirectives(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			},
		},
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "user_id", Type: schema.UUID},
			},
		},
	}
	rels := []SchemaRelationship{
		{ParentTable: "users", ChildTable: "posts", Type: "has_many"},
	}

	src, err := GenerateGraphQLSchema(tables, rels, nil, "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "posts: [Post!]! @auth") {
		t.Error("relationship fields should have @auth")
	}
}

func TestOwnerIDGeneration(t *testing.T) {
	table := &schema.Table{
		Name: "posts",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			{Name: "user_id", Type: schema.UUID, IsOwnerColumn: true},
			{Name: "title", Type: schema.String},
		},
	}

	src, err := GenerateModel(table, "models")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "func (m *Post) OwnerID() string") {
		t.Error("model with IsOwner column should have OwnerID() method")
	}
	if !strings.Contains(s, "m.UserID.String()") {
		t.Error("OwnerID should return the owner column value as string")
	}
}

func TestNoOwnerIDWithoutOwnerColumn(t *testing.T) {
	table := &schema.Table{
		Name: "users",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			{Name: "email", Type: schema.String},
		},
	}

	src, err := GenerateModel(table, "models")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if strings.Contains(s, "OwnerID()") {
		t.Error("model without IsOwner column should not have OwnerID()")
	}
}
