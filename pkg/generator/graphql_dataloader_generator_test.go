package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestDataloaderGeneratorEmpty(t *testing.T) {
	src, err := GenerateGraphQLDataloaders(nil, nil, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "type DataLoaderRegistry struct{}") {
		t.Error("empty registry should be an empty struct")
	}
	if !strings.Contains(s, "func newDataLoaderRegistry(_ VisibilityTier)") {
		t.Error("constructor should accept VisibilityTier even when empty")
	}
}

func TestDataloaderGeneratorHasMany(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "email", Type: schema.String},
			},
		},
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "user_id", Type: schema.UUID},
				{Name: "title", Type: schema.String},
			},
		},
	}
	rels := []SchemaRelationship{
		{ParentTable: "users", ChildTable: "posts", Type: "has_many"},
	}

	src, err := GenerateGraphQLDataloaders(tables, rels, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// Registry field
	if !strings.Contains(s, "postsByUserID") {
		t.Error("registry should have postsByUserID field")
	}

	// Load function name should match resolver convention: load{Child}ListBy{FK}
	if !strings.Contains(s, "func (r *RootResolver) loadPostListByUserID(") {
		t.Error("load function should be named loadPostListByUserID")
	}

	// Batch function
	if !strings.Contains(s, "func (r *DataLoaderRegistry) batchPostsByUserID(") {
		t.Error("batch function should be named batchPostsByUserID")
	}

	// Should use WhereIn
	if !strings.Contains(s, "WhereUserIDIn(ids...)") {
		t.Error("batch function should call WhereUserIDIn")
	}

	// Visibility field on struct
	if !strings.Contains(s, "visibility") || !strings.Contains(s, "VisibilityTier") {
		t.Error("registry should have visibility field")
	}

	// Constructor accepts visibility
	if !strings.Contains(s, "func newDataLoaderRegistry(vis VisibilityTier)") {
		t.Error("constructor should accept VisibilityTier")
	}
}

func TestDataloaderGeneratorHasOne(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			},
		},
		{
			Name: "profiles",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "user_id", Type: schema.UUID},
			},
		},
	}
	rels := []SchemaRelationship{
		{ParentTable: "users", ChildTable: "profiles", Type: "has_one"},
	}

	src, err := GenerateGraphQLDataloaders(tables, rels, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// Load function for has_one: load{Child}By{FK}
	if !strings.Contains(s, "func (r *RootResolver) loadProfileByUserID(") {
		t.Error("has_one load function should be named loadProfileByUserID")
	}

	// Should handle nil record
	if !strings.Contains(s, "if record == nil") {
		t.Error("has_one loader should check for nil record")
	}
}

func TestDataloaderGeneratorVisibility(t *testing.T) {
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
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true, IsPublic: true},
				{Name: "user_id", Type: schema.UUID},
				{Name: "title", Type: schema.String, IsPublic: true},
				{Name: "body", Type: schema.Text, IsOwnerSees: true},
			},
		},
	}
	rels := []SchemaRelationship{
		{ParentTable: "users", ChildTable: "posts", Type: "has_many"},
	}

	src, err := GenerateGraphQLDataloaders(tables, rels, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// Batch function should apply visibility for tables with annotations
	if !strings.Contains(s, "r.visibility") {
		t.Error("batch function should reference r.visibility for tables with visibility annotations")
	}
	if !strings.Contains(s, "SelectPublic()") {
		t.Error("batch function should call SelectPublic for VisibilityPublic")
	}
	if !strings.Contains(s, "SelectOwner()") {
		t.Error("batch function should call SelectOwner for VisibilityOwner")
	}
}

func TestDataloaderGeneratorNoVisibilityWithoutAnnotations(t *testing.T) {
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
				{Name: "title", Type: schema.String},
			},
		},
	}
	rels := []SchemaRelationship{
		{ParentTable: "users", ChildTable: "posts", Type: "has_many"},
	}

	src, err := GenerateGraphQLDataloaders(tables, rels, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// No visibility switch when table has no annotations
	if strings.Contains(s, "SelectPublic()") {
		t.Error("should not call SelectPublic when table has no visibility annotations")
	}
}
