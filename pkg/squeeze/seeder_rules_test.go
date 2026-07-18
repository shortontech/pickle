package squeeze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

func TestRuleSeederUnstableIdentity(t *testing.T) {
	ctx := &AnalysisContext{Seeders: []generator.SeederDefinition{{Name: "CRMSeeder", Kind: "scenario", Policy: "Upsert", File: "crm.go", GraphCalls: []generator.SeederGraphCall{
		{Method: "Create", Line: 10}, {Method: "CreateN", Line: 11}, {Method: "UniqueBy", Line: 10}, {Method: "Update", Line: 10},
	}}}}
	findings := ruleSeederUnstableIdentity(ctx)
	if len(findings) != 1 || findings[0].Rule != "seeder_unstable_identity" {
		t.Fatalf("findings = %#v", findings)
	}
	ctx.Seeders[0].GraphCalls = append(ctx.Seeders[0].GraphCalls, generator.SeederGraphCall{Method: "UniqueBy", Line: 11}, generator.SeederGraphCall{Method: "Update", Line: 11})
	if findings := ruleSeederUnstableIdentity(ctx); len(findings) != 0 {
		t.Fatalf("stable graph findings = %#v", findings)
	}
}

func TestRuleSeederNondeterministic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crm.go")
	if err := os.WriteFile(path, []byte("package seeders\nimport (\"math/rand\"; \"time\")\nfunc x(){ _, _ = rand.Intn(4), time.Now() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := ruleSeederNondeterministic(&AnalysisContext{Seeders: []generator.SeederDefinition{{File: path}}})
	if len(findings) != 2 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestRuleSeederNondeterministicAllowsAnchorContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stable.go")
	if err := os.WriteFile(path, []byte("package seeders\nfunc x(ctx SeedValueContext){ _, _, _ = ctx.AnchorTime, ctx.Past(1), ctx.Future(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if findings := ruleSeederNondeterministic(&AnalysisContext{Seeders: []generator.SeederDefinition{{File: path}}}); len(findings) != 0 {
		t.Fatalf("anchor context findings = %#v", findings)
	}
}

func TestRuleSeederIntegrityOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.go")
	if err := os.WriteFile(path, []byte("package seeders\nfunc x(){ _ = map[string]any{\"row_hash\": []byte{1}} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := ruleSeederIntegrityOverride(&AnalysisContext{Seeders: []generator.SeederDefinition{{File: path}}})
	if len(findings) != 1 || findings[0].Rule != "seeder_integrity_override" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestSeederSchemaRules(t *testing.T) {
	contacts := &schema.Table{Name: "contacts", Columns: []*schema.Column{
		{Name: "id", Type: schema.BigInteger, IsPrimaryKey: true},
		{Name: "name", Type: schema.String},
		{Name: "count", Type: schema.Integer},
	}}
	definition := generator.SeederDefinition{Name: "ContactSeeder", Kind: "row", Table: "contacts", File: "contact.go", Fields: []generator.SeederReturnField{{Name: "name", GoType: "string", Underlying: "string"}, {Name: "count", GoType: "bool", Underlying: "bool"}}}
	ctx := &AnalysisContext{Seeders: []generator.SeederDefinition{definition}, Tables: []*schema.Table{contacts}}
	if findings := ruleSeederTypeMismatch(ctx); len(findings) != 1 || findings[0].Rule != "seeder_type_mismatch" {
		t.Fatalf("type findings = %#v", findings)
	}
	definition.Fields = definition.Fields[:1]
	ctx.Seeders[0] = definition
	if findings := ruleSeederMissingValue(ctx); len(findings) != 1 || !strings.Contains(findings[0].Message, "contacts.count") {
		t.Fatalf("missing findings = %#v", findings)
	}
}

func TestSeederRelationshipRules(t *testing.T) {
	users := &schema.Table{Name: "users", Columns: []*schema.Column{{Name: "id", Type: schema.BigInteger}}}
	contacts := &schema.Table{Name: "contacts", Columns: []*schema.Column{{Name: "owner_id", Type: schema.BigInteger, ForeignKeyTable: "users"}, {Name: "reviewer_id", Type: schema.BigInteger, ForeignKeyTable: "users"}}}
	scenario := generator.SeederDefinition{Name: "CRMSeeder", Kind: "scenario", File: "crm.go", GraphCalls: []generator.SeederGraphCall{{Method: "Create", Arguments: []string{"UserSeederRef"}, Line: 2}, {Method: "Create", Arguments: []string{"ContactSeederRef"}, Line: 3}, {Method: "For", Arguments: []string{"user"}, Line: 3}}}
	ctx := &AnalysisContext{Seeders: []generator.SeederDefinition{scenario, {Name: "UserSeeder", Kind: "row", Table: "users"}, {Name: "ContactSeeder", Kind: "row", Table: "contacts"}}, Tables: []*schema.Table{users, contacts}}
	if findings := ruleSeederAmbiguousRelationship(ctx); len(findings) != 1 {
		t.Fatalf("ambiguous findings = %#v", findings)
	}
	scoped := &schema.Table{Name: "notes", ForeignKeys: []*schema.ForeignKey{{Columns: []string{"tenant_id", "contact_id"}, ReferencedTable: "contacts", ReferencedColumns: []string{"tenant_id", "id"}}}}
	ctx.Tables = append(ctx.Tables, scoped)
	ctx.Seeders[0].GraphCalls = []generator.SeederGraphCall{{Method: "With", Arguments: []string{`"tenant_id"`}, Line: 8}}
	if findings := ruleSeederIncompleteCompositeKey(ctx); len(findings) != 1 {
		t.Fatalf("composite findings = %#v", findings)
	}
}

func TestRuleSeederSensitiveLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.go")
	if err := os.WriteFile(path, []byte(`package seeders
func x(graph *SeedGraph) { graph.Create(UserSeeder).With("api_token", "live-secret") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := ruleSeederSensitiveLiteral(&AnalysisContext{Seeders: []generator.SeederDefinition{{File: path}}})
	if len(findings) != 1 || findings[0].Rule != "seeder_sensitive_literal" {
		t.Fatalf("findings = %#v", findings)
	}
}
