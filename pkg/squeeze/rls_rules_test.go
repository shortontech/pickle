package squeeze

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
)

func TestRLSGuidanceWarnsForRLSMigration(t *testing.T) {
	ctx := &AnalysisContext{Migrations: []generator.MigrationOps{{
		Name: "CreateIsolation_2026_07_16_120000",
		File: "2026_07_16_120000_create_isolation.go",
		Up: []generator.MigrationOperation{
			{Type: "enable_rls", Table: "messages"},
			{Type: "force_rls", Table: "messages"},
			{Type: "create_rls_policy", Table: "messages"},
		},
	}}}
	findings := ruleRLSGuidance(ctx)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want one: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Rule != "rls_guidance" || f.Severity != SeverityWarning {
		t.Fatalf("unexpected finding: %+v", f)
	}
	if f.File != "database/migrations/2026_07_16_120000_create_isolation.go" {
		t.Errorf("unexpected file: %q", f.File)
	}
	for _, phrase := range []string{"Pickle policies", "static security guarantees", "Raw SQL in regular application code is a Squeeze error", "not a reason to choose RLS", "RawSQL remains acceptable inside migrations"} {
		if !strings.Contains(f.Message, phrase) {
			t.Errorf("message missing %q: %s", phrase, f.Message)
		}
	}
}

func TestRLSGuidanceWarnsOncePerProject(t *testing.T) {
	ctx := &AnalysisContext{Migrations: []generator.MigrationOps{
		{Name: "First", Up: []generator.MigrationOperation{{Type: "enable_rls"}}},
		{Name: "Second", Up: []generator.MigrationOperation{{Type: "create_rls_policy"}}},
	}}
	if findings := ruleRLSGuidance(ctx); len(findings) != 1 {
		t.Fatalf("got %d findings, want one", len(findings))
	}
}

func TestRLSGuidanceIgnoresPolicyFreeMigration(t *testing.T) {
	ctx := &AnalysisContext{Migrations: []generator.MigrationOps{{Name: "CreateMessages", Up: []generator.MigrationOperation{{Type: "create_table"}}}}}
	if findings := ruleRLSGuidance(ctx); len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}

func TestRLSGuidanceRecognizesLegacyRawSQLPolicyMigration(t *testing.T) {
	ctx := &AnalysisContext{Migrations: []generator.MigrationOps{{
		Name: "CreateIsolation_2026_07_16_120000",
		Up: []generator.MigrationOperation{{Type: "raw_sql", SQL: `ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
CREATE POLICY message_scope ON messages USING (true)`}},
	}}}
	if findings := ruleRLSGuidance(ctx); len(findings) != 1 {
		t.Fatalf("got %d findings, want one", len(findings))
	}
}
