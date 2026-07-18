package squeeze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
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
