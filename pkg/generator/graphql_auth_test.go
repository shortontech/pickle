package generator

import (
	"os"
	"path/filepath"
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
	if !strings.Contains(s, "id: ID! @auth") {
		t.Error("unannotated PK should have @auth")
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

func TestGraphQLSchemaExplicitPublicPrimaryKey(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "posts",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.UUID, IsPrimaryKey: true, IsPublic: true},
				{Name: "title", Type: schema.String},
			},
		},
	}

	sdl := BuildSDL(tables, nil, nil)
	if !strings.Contains(sdl, "id: ID! @public") {
		t.Error("explicitly public PK should have @public")
	}
}

func TestGraphQLResolverRequiresAuthForUnannotatedPrimaryKey(t *testing.T) {
	table := &schema.Table{
		Name: "users",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
		},
	}

	src, err := GenerateGraphQLResolversWithPlans([]GraphQLModelPlan{{
		Table:      table,
		Operations: map[string]bool{"show": true},
	}}, nil, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, `case "id":`) {
		t.Fatal("resolver should include id field")
	}
	idCase := s[strings.Index(s, `case "id":`):]
	if !strings.Contains(idCase, "!ctx.IsAuthenticated()") {
		t.Error("unannotated primary key field should require authentication")
	}
}

func TestGraphQLDateTimeInclusiveFiltersUseInclusiveScopes(t *testing.T) {
	table := &schema.Table{
		Name: "events",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			{Name: "created_at", Type: schema.Timestamp},
		},
	}

	src, err := GenerateGraphQLResolversWithPlans([]GraphQLModelPlan{{
		Table:      table,
		Operations: map[string]bool{"list": true},
	}}, nil, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)

	if !strings.Contains(s, `fm["gte"].(string)`) || !strings.Contains(s, "q.WhereCreatedAtGTE(t)") {
		t.Fatalf("expected GraphQL datetime gte filter to call WhereCreatedAtGTE\n%s", s)
	}
	if !strings.Contains(s, `fm["lte"].(string)`) || !strings.Contains(s, "q.WhereCreatedAtLTE(t)") {
		t.Fatalf("expected GraphQL datetime lte filter to call WhereCreatedAtLTE\n%s", s)
	}
}

func TestGraphQLSchemaExcludesAuthCredentialIdentifiers(t *testing.T) {
	tables := []*schema.Table{
		{
			Name: "jwt_tokens",
			Columns: []*schema.Column{
				{Name: "jti", Type: schema.String, IsPrimaryKey: true},
				{Name: "user_id", Type: schema.UUID},
			},
		},
		{
			Name: "oauth_tokens",
			Columns: []*schema.Column{
				{Name: "token", Type: schema.String, IsPrimaryKey: true},
				{Name: "client_id", Type: schema.String},
			},
		},
		{
			Name: "sessions",
			Columns: []*schema.Column{
				{Name: "id", Type: schema.String, IsPrimaryKey: true},
				{Name: "user_id", Type: schema.UUID},
			},
		},
	}

	sdl := BuildSDL(tables, nil, nil)
	for _, forbidden := range []string{
		"jti:",
		"JTI_ASC",
		"token:",
		"TOKEN_ASC",
		"id: String!",
		"  ID_ASC\n",
	} {
		if strings.Contains(sdl, forbidden) {
			t.Fatalf("auth credential identifier leaked into SDL: %q\n%s", forbidden, sdl)
		}
	}
	if !strings.Contains(sdl, "userId: ID! @auth") {
		t.Error("non-credential auth table fields should still be emitted when table is exposed")
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

func TestGraphQLOperationExposureListOnly(t *testing.T) {
	table := &schema.Table{
		Name: "users",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			{Name: "name", Type: schema.String},
		},
	}
	plans := []GraphQLModelPlan{{
		Table:      table,
		Operations: map[string]bool{"list": true},
	}}

	sdl := BuildSDLWithPlans(plans, nil, nil)
	if !strings.Contains(sdl, "users(filter: UserFilter") {
		t.Error("list exposure should generate plural query")
	}
	if strings.Contains(sdl, "user(id: ID!)") {
		t.Error("list-only exposure should not generate show query")
	}
	if strings.Contains(sdl, "createUser") || strings.Contains(sdl, "updateUser") || strings.Contains(sdl, "deleteUser") {
		t.Error("list-only exposure should not generate mutations")
	}

	resolvers, err := GenerateGraphQLResolversWithPlans(plans, nil, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	resolverSrc := string(resolvers)
	if !strings.Contains(resolverSrc, `case "users":`) {
		t.Error("resolver should dispatch list query")
	}
	if strings.Contains(resolverSrc, `case "user":`) {
		t.Error("resolver should not dispatch show query")
	}
}

func TestGraphQLOperationExposureMutationOnly(t *testing.T) {
	table := &schema.Table{
		Name: "posts",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			{Name: "title", Type: schema.String},
		},
	}
	plans := []GraphQLModelPlan{{
		Table:      table,
		Operations: map[string]bool{"create": true, "update": true},
	}}

	sdl := BuildSDLWithPlans(plans, nil, nil)
	if strings.Contains(sdl, "posts(filter: PostFilter") || strings.Contains(sdl, "post(id: ID!)") {
		t.Error("mutation-only exposure should not generate queries")
	}
	if !strings.Contains(sdl, "createPost(input: CreatePostInput!): Post! @auth") {
		t.Error("create exposure should generate create mutation")
	}
	if !strings.Contains(sdl, "updatePost(id: ID!, input: UpdatePostInput!): Post! @auth") {
		t.Error("update exposure should generate update mutation")
	}
	if strings.Contains(sdl, "deletePost") {
		t.Error("mutation plan without delete should not generate delete mutation")
	}

	mutations, err := GenerateGraphQLMutationsWithPlans(plans, "myapp/app/models", "graphql")
	if err != nil {
		t.Fatal(err)
	}
	mutationSrc := string(mutations)
	if !strings.Contains(mutationSrc, `case "createPost":`) || !strings.Contains(mutationSrc, `case "updatePost":`) {
		t.Error("mutation dispatch should include exposed mutation operations")
	}
	if strings.Contains(mutationSrc, `case "deletePost":`) {
		t.Error("mutation dispatch should omit unexposed delete")
	}

	crud, err := GenerateGraphQLCRUDResolvers(CRUDConfig{
		Tables:       []*schema.Table{table},
		ModelsImport: "myapp/app/models",
		PackageName:  "graphql",
		Plans:        plans,
	})
	if err != nil {
		t.Fatal(err)
	}
	crudSrc := string(crud)
	if !strings.Contains(crudSrc, "crudCreatePost") || !strings.Contains(crudSrc, "crudUpdatePost") {
		t.Error("CRUD resolver should include exposed create/update")
	}
	if strings.Contains(crudSrc, "crudDeletePost") {
		t.Error("CRUD resolver should omit unexposed delete")
	}
}

func TestGraphQLLegacyOperationExposureStillFullCRUD(t *testing.T) {
	table := &schema.Table{
		Name: "posts",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.UUID, IsPrimaryKey: true},
			{Name: "title", Type: schema.String},
		},
	}
	sdl := BuildSDL([]*schema.Table{table}, nil, nil)
	for _, want := range []string{
		"posts(filter: PostFilter",
		"post(id: ID!)",
		"createPost(input: CreatePostInput!): Post! @auth",
		"updatePost(id: ID!, input: UpdatePostInput!): Post! @auth",
		"deletePost(id: ID!): Boolean! @auth",
	} {
		if !strings.Contains(sdl, want) {
			t.Errorf("legacy GraphQL should include %q", want)
		}
	}
}

func TestDeriveGraphQLStateIgnoresDownBody(t *testing.T) {
	dir := t.TempDir()
	src := `package graphql

type PublicAPI struct { GraphQLPolicy }

func (p *PublicAPI) Up() {
	p.Expose("users", func(e *ExposeBuilder) {
		e.List()
		e.Show()
	})
}

func (p *PublicAPI) Down() {
	p.Unexpose("users")
}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_06_02_100000_public_api.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	state := DeriveGraphQLStateFromDir(dir)
	if len(state.Exposures) != 1 {
		t.Fatalf("expected 1 exposure, got %#v", state.Exposures)
	}
	if state.Exposures[0].Model != "users" {
		t.Fatalf("model = %q, want users", state.Exposures[0].Model)
	}
	if got := strings.Join(state.Exposures[0].Operations, ","); got != "list,show" {
		t.Fatalf("operations = %q, want list,show", got)
	}
}

func TestDeriveGraphQLStateRelationshipBudgets(t *testing.T) {
	dir := t.TempDir()
	src := `package graphql

type PublicAPI struct { GraphQLPolicy }

func (p *PublicAPI) Up() {
	p.Expose("users", func(e *ExposeBuilder) {
		e.List()
		e.Relationship("posts", func(r *RelationshipExposure) {
			r.Cost(13)
			r.MaxPageSize(55)
		})
	})
}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_06_02_100000_public_api.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	state := DeriveGraphQLStateFromDir(dir)
	if len(state.Exposures) != 1 {
		t.Fatalf("expected 1 exposure, got %#v", state.Exposures)
	}
	rels := state.Exposures[0].Relationships
	if len(rels) != 1 {
		t.Fatalf("expected 1 relationship exposure, got %#v", rels)
	}
	if rels[0].Name != "posts" || rels[0].Cost != 13 || rels[0].MaxPageSize != 55 {
		t.Fatalf("relationship = %#v", rels[0])
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
