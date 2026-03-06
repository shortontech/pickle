package generator

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
	"github.com/shortontech/pickle/pkg/tickle"
)

func loadScopeBlocks(t *testing.T) []tickle.ScopeBlock {
	t.Helper()
	scopesPath := filepath.Join("..", "..", "pkg", "cooked", "scopes.go")
	blocks, err := tickle.ParseScopeBlocks(scopesPath)
	if err != nil {
		t.Fatalf("parsing scope blocks: %v", err)
	}
	return blocks
}

// TestSearchNullableString ensures nullable string columns emit &p instead of
// passing a raw string to Where* methods that expect *string.
func TestSearchNullableString(t *testing.T) {
	tbl := &schema.Table{Name: "transfers"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("status").NotNull()
	tbl.String("brale_transfer_id", 255).Nullable()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	// Must parse as valid Go
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "transfer_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	// The nullable string filter should use &p, not pass val directly
	if strings.Contains(src, "q.WhereBraleTransferId(val)") {
		t.Error("nullable string column should not pass val directly — needs pointer")
	}
	if !strings.Contains(src, "&p") {
		t.Errorf("expected &p for nullable string filter\n%s", src)
	}
}

// TestSearchNullableTimestamp ensures nullable timestamp columns emit &parsed
// instead of passing a raw time.Time to Where* methods that expect *time.Time.
func TestSearchNullableTimestamp(t *testing.T) {
	tbl := &schema.Table{Name: "jwt_tokens"}
	tbl.String("jti").PrimaryKey()
	tbl.Timestamp("revoked_at").Nullable()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "jwt_token_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	// WhereRevokedAt expects *time.Time, so search must pass &parsed
	if strings.Contains(src, "q.WhereRevokedAt(parsed)") {
		t.Error("nullable timestamp should use &parsed, not parsed")
	}
	if !strings.Contains(src, "q.WhereRevokedAt(&parsed)") {
		t.Errorf("expected q.WhereRevokedAt(&parsed) for nullable timestamp\n%s", src)
	}
}

// TestSearchNullableUUID ensures nullable UUID columns emit &parsed.
func TestSearchNullableUUID(t *testing.T) {
	tbl := &schema.Table{Name: "orders"}
	tbl.UUID("id").PrimaryKey()
	tbl.UUID("parent_id").Nullable()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "order_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	if strings.Contains(src, "q.WhereParentID(parsed)\n") {
		// Check the exact-match case, not the operator cases
		// Need to be careful — non-nullable WhereID(parsed) is fine
		lines := strings.Split(src, "\n")
		for i, line := range lines {
			if strings.Contains(line, "q.WhereParentID(parsed)") {
				t.Errorf("line %d: nullable UUID should use &parsed, not parsed: %s", i+1, line)
			}
		}
	}
}

// TestSearchJSONBExcluded ensures JSONB columns are not included in search switch.
func TestSearchJSONBExcluded(t *testing.T) {
	tbl := &schema.Table{Name: "events"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("name").NotNull()
	tbl.JSONB("metadata").Nullable()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "event_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	// JSONB should not appear as a case in any Search method's filter switch
	if strings.Contains(src, `case "metadata"`) {
		t.Error("JSONB column should not be included in search filter switch")
	}
}

// TestSearchNoPublicAnyMethods ensures the generated code never calls
// the exported Where/WhereOp/WhereIn/WhereNotIn (capital W) methods.
func TestSearchNoPublicAnyMethods(t *testing.T) {
	tbl := &schema.Table{Name: "users"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("name", 255).NotNull()
	tbl.String("email", 255).NotNull()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	// Should never contain q.Where(" (the generic any-accepting method)
	// but should contain q.where(" (the unexported internal method)
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip method declarations (func ... Where...)
		if strings.HasPrefix(trimmed, "func ") {
			continue
		}
		// Check for calls to the old exported methods
		if strings.Contains(trimmed, ".Where(") || strings.Contains(trimmed, ".WhereOp(") ||
			strings.Contains(trimmed, ".WhereIn(") || strings.Contains(trimmed, ".WhereNotIn(") {
			t.Errorf("generated code calls exported any-accepting method: %s", trimmed)
		}
	}
}

// TestSearchVisibilityLevels ensures models with visibility annotations
// get SearchPublic/SearchOwner/SearchAll, and models without only get SearchAll.
func TestSearchVisibilityLevels(t *testing.T) {
	blocks := loadScopeBlocks(t)

	t.Run("with visibility annotations", func(t *testing.T) {
		tbl := &schema.Table{Name: "posts"}
		tbl.UUID("id").PrimaryKey().Public()
		tbl.UUID("user_id").NotNull().ForeignKey("users", "id")
		tbl.String("title", 255).NotNull().Public()
		tbl.Text("body").NotNull().OwnerSees()
		tbl.Timestamps()

		out, err := GenerateQueryScopes(tbl, blocks, "models")
		if err != nil {
			t.Fatalf("GenerateQueryScopes: %v", err)
		}
		src := string(out)

		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, "post_query.go", src, parser.AllErrors); err != nil {
			t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
		}

		if !strings.Contains(src, "func (q *PostQuery) SearchPublic(") {
			t.Error("missing SearchPublic")
		}
		if !strings.Contains(src, "func (q *PostQuery) SearchOwner(") {
			t.Error("missing SearchOwner")
		}
		if !strings.Contains(src, "func (q *PostQuery) SearchAll(") {
			t.Error("missing SearchAll")
		}

		// SearchPublic should not include body (OwnerSees) or user_id (no annotation)
		pubIdx := strings.Index(src, "func (q *PostQuery) SearchPublic(")
		ownerIdx := strings.Index(src, "func (q *PostQuery) SearchOwner(")
		publicBody := src[pubIdx:ownerIdx]
		if strings.Contains(publicBody, `case "body"`) {
			t.Error("SearchPublic should not include OwnerSees column 'body'")
		}
		if strings.Contains(publicBody, `case "user_id"`) {
			t.Error("SearchPublic should not include unannotated column 'user_id'")
		}

		// SearchOwner should include body but not user_id
		allIdx := strings.Index(src, "func (q *PostQuery) SearchAll(")
		ownerBody := src[ownerIdx:allIdx]
		if !strings.Contains(ownerBody, `case "body"`) {
			t.Error("SearchOwner should include OwnerSees column 'body'")
		}
		if strings.Contains(ownerBody, `case "user_id"`) {
			t.Error("SearchOwner should not include unannotated column 'user_id'")
		}
	})

	t.Run("without visibility annotations", func(t *testing.T) {
		tbl := &schema.Table{Name: "users"}
		tbl.UUID("id").PrimaryKey()
		tbl.String("name", 255).NotNull()
		tbl.Timestamps()

		out, err := GenerateQueryScopes(tbl, blocks, "models")
		if err != nil {
			t.Fatalf("GenerateQueryScopes: %v", err)
		}
		src := string(out)

		if strings.Contains(src, "SearchPublic(") {
			t.Error("model without visibility annotations should not have SearchPublic")
		}
		if strings.Contains(src, "SearchOwner(") {
			t.Error("model without visibility annotations should not have SearchOwner")
		}
		if !strings.Contains(src, "SearchAll(") {
			t.Error("all models should have SearchAll")
		}
	})
}

// TestSearchSelectVisibility ensures SelectPublic/SelectOwner are generated
// for models with visibility annotations and SelectAll is always present.
func TestSearchSelectVisibility(t *testing.T) {
	blocks := loadScopeBlocks(t)

	tbl := &schema.Table{Name: "posts"}
	tbl.UUID("id").PrimaryKey().Public()
	tbl.String("title", 255).NotNull().Public()
	tbl.Text("body").NotNull().OwnerSees()
	tbl.String("internal_notes").NotNull()
	tbl.Timestamps()

	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}
	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "post_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	if !strings.Contains(src, "func (q *PostQuery) SelectPublic()") {
		t.Error("missing SelectPublic")
	}
	if !strings.Contains(src, "func (q *PostQuery) SelectOwner()") {
		t.Error("missing SelectOwner")
	}
	if !strings.Contains(src, "func (q *PostQuery) SelectAll()") {
		t.Error("missing SelectAll")
	}

	// SelectPublic should only have id, title (the Public columns)
	pubIdx := strings.Index(src, "func (q *PostQuery) SelectPublic()")
	ownerIdx := strings.Index(src, "func (q *PostQuery) SelectOwner()")
	pubBody := src[pubIdx:ownerIdx]
	if strings.Contains(pubBody, `"body"`) {
		t.Error("SelectPublic should not include OwnerSees column")
	}
	if strings.Contains(pubBody, `"internal_notes"`) {
		t.Error("SelectPublic should not include unannotated column")
	}

	// SelectOwner should have id, title, body but not internal_notes
	allIdx := strings.Index(src, "func (q *PostQuery) SelectAll()")
	ownerBody := src[ownerIdx:allIdx]
	if !strings.Contains(ownerBody, `"body"`) {
		t.Error("SelectOwner should include OwnerSees column")
	}
	if strings.Contains(ownerBody, `"internal_notes"`) {
		t.Error("SelectOwner should not include unannotated column")
	}
}

// TestSearchNonNullablePassedDirectly ensures non-nullable columns
// pass values directly (not via pointer).
func TestSearchNonNullablePassedDirectly(t *testing.T) {
	tbl := &schema.Table{Name: "users"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("email", 255).NotNull()
	tbl.Timestamp("created_at").NotNull()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "user_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	// Non-nullable email should be passed directly
	if !strings.Contains(src, "q.WhereEmail(val)") {
		t.Error("non-nullable string should pass val directly")
	}
	// Non-nullable UUID should be passed directly
	if !strings.Contains(src, "q.WhereID(parsed)") {
		t.Error("non-nullable UUID should pass parsed directly")
	}
}

// TestSearchAllColumnTypes ensures search handles all column types without
// generating code that fails to parse.
func TestSearchAllColumnTypes(t *testing.T) {
	tbl := &schema.Table{Name: "kitchen_sinks"}
	tbl.UUID("id").PrimaryKey()
	tbl.String("name", 255).NotNull()
	tbl.Text("description").Nullable()
	tbl.Integer("count").NotNull()
	tbl.BigInteger("big_count").Nullable()
	tbl.Decimal("price", 18, 2).NotNull()
	tbl.Boolean("active").NotNull()
	tbl.Timestamp("starts_at").NotNull()
	tbl.Timestamp("ends_at").Nullable()
	tbl.Date("birthday").NotNull()
	tbl.JSONB("metadata").Nullable()
	tbl.Timestamps()

	blocks := loadScopeBlocks(t)
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}

	src := string(out)

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "kitchen_sink_query.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated code does not parse:\n%v\n%s", err, src)
	}

	// Verify SearchAll exists and includes the right columns
	if !strings.Contains(src, "func (q *KitchenSinkQuery) SearchAll(") {
		t.Error("missing SearchAll")
	}

	// Non-searchable columns should be excluded
	if strings.Contains(src, `case "metadata"`) {
		t.Error("JSONB should be excluded from search")
	}

	// Nullable fields should use pointer passing
	if strings.Contains(src, "q.WhereEndsAt(parsed)") {
		t.Error("nullable timestamp ends_at should use &parsed")
	}
	if strings.Contains(src, "q.WhereDescription(val)") {
		t.Error("nullable string description should use &p")
	}
}
