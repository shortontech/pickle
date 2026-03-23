package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestCRUDResolverGeneration(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "user_id", Type: schema.UUID, IsOwnerColumn: true},
				{Name: "title", Type: schema.String, Length: 255},
				{Name: "body", Type: schema.Text},
				{Name: "created_at", Type: schema.Timestamp},
				{Name: "updated_at", Type: schema.Timestamp},
			},
		},
	}

	src, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
		Tables:       tables,
		ModelsImport: "myapp/app/models",
		PackageName:  "graphql",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// Should have ownership scoping on create
	if !strings.Contains(s, "ownerID, err := uuid.Parse(ctx.UserID())") {
		t.Error("create should set owner from auth context")
	}
	if !strings.Contains(s, "record.UserID = ownerID") {
		t.Error("create should assign owner ID to record")
	}

	// Should have ownership scoping on update
	if !strings.Contains(s, "crudUpdate") {
		t.Error("should generate crudUpdate method")
	}
	if !strings.Contains(s, "q.WhereUserID(ownerID)") {
		t.Error("update should scope by owner")
	}

	// Should have ownership scoping on delete
	if !strings.Contains(s, "crudDelete") {
		t.Error("should generate crudDelete method")
	}

	// Should have constraint validation
	if !strings.Contains(s, "validatePostConstraints") {
		t.Error("should generate constraint validator")
	}
}

func TestCRUDResolverWithoutOwnership(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "categories",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String, Length: 100},
				{Name: "created_at", Type: schema.Timestamp},
			},
		},
	}

	src, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
		Tables:       tables,
		ModelsImport: "myapp/app/models",
		PackageName:  "graphql",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// Should require auth but NOT scope by owner
	if !strings.Contains(s, "ctx.IsAuthenticated()") {
		t.Error("should require authentication")
	}
	if strings.Contains(s, "ownerID") {
		t.Error("should not have owner scoping for table without IsOwner column")
	}
}

func TestNestedCreateMutation(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String},
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

	src, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
		Tables:        tables,
		Relationships: rels,
		ModelsImport:  "myapp/app/models",
		PackageName:   "graphql",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, "crudCreateNestedPost") {
		t.Error("should generate nested create mutation for child table")
	}
	if !strings.Contains(s, "record.UserID = parentID") {
		t.Error("nested create should set FK from parent")
	}
	if !strings.Contains(s, `field.Args["userId"]`) {
		t.Error("nested create should extract parent ID from args")
	}
}

func TestConstraintValidation(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "title", Type: schema.String, Length: 255},
				{Name: "category_id", Type: schema.UUID, ForeignKeyTable: "categories", ForeignKeyColumn: "id"},
			},
		},
	}

	src, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
		Tables:       tables,
		ModelsImport: "myapp/app/models",
		PackageName:  "graphql",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	// String length constraint
	if !strings.Contains(s, "maximum length 255") {
		t.Error("should validate string length from column constraint")
	}
	// UUID format for FK
	if !strings.Contains(s, "invalid UUID format") {
		t.Error("should validate UUID format for FK columns")
	}
}

func TestColumnValidationMap(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "users",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "name", Type: schema.String, Length: 255},
				{Name: "email", Type: schema.String},
			},
		},
	}

	m := BuildColumnValidationMap(tables)

	nameTag := m["CreateUserInput.Name"]
	if !strings.Contains(nameTag, "required") {
		t.Errorf("name should be required, got %q", nameTag)
	}
	if !strings.Contains(nameTag, "max=255") {
		t.Errorf("name should have max=255, got %q", nameTag)
	}

	// Update should not have required
	updateNameTag := m["UpdateUserInput.Name"]
	if strings.Contains(updateNameTag, "required") {
		t.Errorf("update name should not be required, got %q", updateNameTag)
	}
}

func TestHasCRUDOverride(t *testing.T) {
	// Non-existent directory should not have override
	if HasCRUDOverride("/nonexistent", "users") {
		t.Error("should return false for non-existent directory")
	}
}

func TestMergeValidationMaps(t *testing.T) {
	reqMap := RequestValidationMap{
		"CreateUserInput.Name": "required,min=1,max=255",
	}
	colMap := RequestValidationMap{
		"CreateUserInput.Name":  "required,max=255",
		"CreateUserInput.Email": "required",
	}

	merged := MergeValidationMaps(reqMap, colMap)

	// Request-based should override column-based
	if merged["CreateUserInput.Name"] != "required,min=1,max=255" {
		t.Errorf("request tag should take precedence, got %q", merged["CreateUserInput.Name"])
	}
	// Column-only should remain
	if merged["CreateUserInput.Email"] != "required" {
		t.Errorf("column tag should be preserved, got %q", merged["CreateUserInput.Email"])
	}
}

func TestColumnConstraints(t *testing.T) {
	tests := []struct {
		name string
		col  *schema.Column
		want string
	}{
		{
			name: "required string with length",
			col:  &schema.Column{Name: "title", Type: schema.String, Length: 255},
			want: "required,max=255",
		},
		{
			name: "nullable string",
			col:  &schema.Column{Name: "bio", Type: schema.String, IsNullable: true, Length: 500},
			want: "max=500",
		},
		{
			name: "uuid column",
			col:  &schema.Column{Name: "ref_id", Type: schema.UUID},
			want: "required,uuid",
		},
		{
			name: "string with default",
			col:  &schema.Column{Name: "status", Type: schema.String, DefaultValue: "draft"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ColumnConstraints(tt.col)
			if got != tt.want {
				t.Errorf("ColumnConstraints() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNestedMutationSDL(t *testing.T) {
	tables := []*schema.Table{
		{Name: "users", Columns: []*schema.Column{{Name: "id", Type: schema.UUID, IsPrimaryKey: true}}},
		{Name: "posts", Columns: []*schema.Column{{Name: "id", Type: schema.UUID, IsPrimaryKey: true}}},
	}
	rels := []SchemaRelationship{
		{ParentTable: "users", ChildTable: "posts", Type: "has_many"},
	}

	sdl := GenerateNestedMutationSDL(tables, rels)
	if !strings.Contains(sdl, "createNestedPost(userId: ID!") {
		t.Errorf("should generate nested mutation SDL, got: %s", sdl)
	}
}

func TestImmutableTableSkipped(t *testing.T) {
	tables := []*schema.Table{
		{
			Name:        "audit_logs",
			IsImmutable: true,
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
				{Name: "action", Type: schema.String},
			},
		},
	}

	src, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
		Tables:       tables,
		ModelsImport: "myapp/app/models",
		PackageName:  "graphql",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if strings.Contains(s, "crudCreate") {
		t.Error("immutable table should not have create mutation")
	}
}
