package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pickle-framework/pickle/pkg/schema"
	"github.com/pickle-framework/pickle/pkg/tickler"
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
	outputDir := filepath.Join("..", "..", "testdata", "basic-crud", "generated", "models")
	os.MkdirAll(outputDir, 0o755)

	for _, tbl := range basicCrudTables() {
		out, err := GenerateModel(tbl, "models")
		if err != nil {
			t.Fatalf("generating model for %s: %v", tbl.Name, err)
		}

		filename := tableToStructName(tbl.Name)
		outPath := filepath.Join(outputDir, toLowerFirst(filename)+".go")
		if err := os.WriteFile(outPath, out, 0o644); err != nil {
			t.Fatalf("writing %s: %v", outPath, err)
		}
		t.Logf("generated → %s", outPath)
	}
}

func TestGenerateBasicCrudScopes(t *testing.T) {
	scopesPath := filepath.Join("..", "..", "pkg", "cooked", "scopes.go")
	blocks, err := tickler.ParseScopeBlocks(scopesPath)
	if err != nil {
		t.Fatalf("parsing scope blocks: %v", err)
	}

	outputDir := filepath.Join("..", "..", "testdata", "basic-crud", "generated", "queries")
	os.MkdirAll(outputDir, 0o755)

	for _, tbl := range basicCrudTables() {
		out, err := GenerateQueryScopes(tbl, blocks, "queries")
		if err != nil {
			t.Fatalf("generating scopes for %s: %v", tbl.Name, err)
		}

		filename := tableToStructName(tbl.Name)
		outPath := filepath.Join(outputDir, toLowerFirst(filename)+"_query.go")
		if err := os.WriteFile(outPath, out, 0o644); err != nil {
			t.Fatalf("writing %s: %v", outPath, err)
		}
		t.Logf("generated → %s", outPath)

		// Verify key methods exist
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
	}
}

func toLowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(s[0]+32) + s[1:]
}
