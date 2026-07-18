package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
	"github.com/shortontech/pickle/pkg/tickle"
)

func basicCrudTables() []*schema.Table {
	var usersMig schema.Migration
	usersMig.CreateTable("users", func(tbl *schema.Table) {
		tbl.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		tbl.String("name", 255).NotNull()
		tbl.String("email", 255).NotNull().Unique()
		tbl.String("password", 255).NotNull()
		tbl.Timestamps()
	})

	var postsMig schema.Migration
	postsMig.CreateTable("posts", func(tbl *schema.Table) {
		tbl.UUID("id").PrimaryKey().Default("uuid_generate_v7()")
		tbl.UUID("user_id").NotNull().ForeignKey("users", "id")
		tbl.String("title", 255).NotNull()
		tbl.Text("body").NotNull()
		tbl.String("status").NotNull().Default("draft")
		tbl.Timestamps()
	})

	var tables []*schema.Table
	for _, mig := range []schema.Migration{usersMig, postsMig} {
		for _, op := range mig.Operations {
			if op.Type == schema.OpCreateTable {
				tables = append(tables, op.TableDef)
			}
		}
	}
	return tables
}

func TestGenerateBasicCrudModels(t *testing.T) {
	for _, tbl := range basicCrudTables() {
		out, err := GenerateModel(tbl, "models")
		if err != nil {
			t.Fatalf("generating model for %s: %v", tbl.Name, err)
		}

		filename := tableToStructName(tbl.Name)
		t.Logf("generated %s (%d bytes)", toLowerFirst(filename)+".go", len(out))
	}
}

func TestGenerateBasicCrudScopes(t *testing.T) {
	scopesPath := filepath.Join("..", "..", "pkg", "cooked", "scopes.go")
	blocks, err := tickle.ParseScopeBlocks(scopesPath)
	if err != nil {
		t.Fatalf("parsing scope blocks: %v", err)
	}

	for _, tbl := range basicCrudTables() {
		out, err := GenerateQueryScopes(tbl, blocks, "models")
		if err != nil {
			t.Fatalf("generating scopes for %s: %v", tbl.Name, err)
		}

		src := string(out)

		if !strings.Contains(src, "WhereID(") {
			t.Errorf("missing WhereID scope for %s", tbl.Name)
		}
		if !strings.Contains(src, "WhereIDNot(") {
			t.Errorf("missing WhereIDNot scope for %s", tbl.Name)
		}

		// Posts should have WithUser from foreign key
		if tbl.Name == "posts" && !strings.Contains(src, "WithUser()") {
			t.Error("missing WithUser eager loading for posts")
		}

		t.Logf("generated %s_query.go (%d bytes)", tbl.Name, len(out))
	}
}

func TestGenerateScopesDisambiguatesRepeatedForeignKeyTargets(t *testing.T) {
	tbl := &schema.Table{Name: "resource_links", Columns: []*schema.Column{
		{Name: "resource_type_code", ForeignKeyTable: "resource_type_registry", ForeignKeyColumn: "code"},
		{Name: "reference_resource_type_code", ForeignKeyTable: "resource_type_registry", ForeignKeyColumn: "code"},
	}}
	scopesPath := filepath.Join("..", "..", "pkg", "cooked", "scopes.go")
	blocks, err := tickle.ParseScopeBlocks(scopesPath)
	if err != nil {
		t.Fatalf("parsing scope blocks: %v", err)
	}
	out, err := GenerateQueryScopes(tbl, blocks, "models")
	if err != nil {
		t.Fatalf("GenerateQueryScopes: %v", err)
	}
	src := string(out)
	for _, method := range []string{"WithResourceTypeCode()", "WithReferenceResourceTypeCode()"} {
		if strings.Count(src, method) != 1 {
			t.Errorf("expected one %s method, generated:\n%s", method, src)
		}
	}
	if strings.Contains(src, "WithResourceTypeRegistry()") {
		t.Errorf("repeated FK target retained ambiguous method name:\n%s", src)
	}
}
