package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

func rowPolicyMethod(t *testing.T, body string) *ControllerMethod {
	t.Helper()
	src := "package controllers\nfunc (c C) Show(){" + body + "}"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "controller.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	return &ControllerMethod{File: "controller.go", Fset: fset, Body: fn.Body}
}
func protectedMessages() []generator.ResolvedRowPolicy {
	return []generator.ResolvedRowPolicy{{Protection: schema.RowProtection{Table: "messages"}, EnforcementClass: "portable"}}
}

func TestRowPolicyInvalidFinding(t *testing.T) {
	findings := ruleRowPolicyInvalid(&AnalysisContext{RowPolicyError: "unknown identity"})
	if len(findings) != 1 || findings[0].Severity != SeverityError {
		t.Fatalf("unexpected: %+v", findings)
	}
}
func TestRowPolicyContextMissingFindsDirectQuery(t *testing.T) {
	method := rowPolicyMethod(t, `models.QueryMessage().All()`)
	findings := ruleRowPolicyContextMissing(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}, RowPolicies: protectedMessages()})
	if len(findings) != 1 {
		t.Fatalf("unexpected: %+v", findings)
	}
}
func TestRowPolicyContextMissingAcceptsExplicitContext(t *testing.T) {
	method := rowPolicyMethod(t, `models.QueryMessage().WithPolicyContext(policyContext).All()`)
	findings := ruleRowPolicyContextMissing(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}, RowPolicies: protectedMessages()})
	if len(findings) != 0 {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestPolicyContextAnalysisIncludesJobsAndCommands(t *testing.T) {
	root := t.TempDir()
	for _, entry := range []struct{ dir, file, source string }{
		{"jobs", "cleanup.go", "package jobs\ntype Cleanup struct{}\nfunc(Cleanup) Handle() error { models.QueryMessage().All(); return nil }"},
		{"commands", "report.go", "package commands\ntype Report struct{}\nfunc(Report) Run(args []string) error { models.QueryMessage().WithPolicyContext(policyContext).All(); return nil }"},
	} {
		dir := filepath.Join(root, "app", entry.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.file), []byte(entry.source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	methods := map[string]*ControllerMethod{}
	if err := mergePolicyEntryMethods(root, methods); err != nil {
		t.Fatal(err)
	}
	findings := ruleRowPolicyContextMissing(&AnalysisContext{Methods: methods, RowPolicies: protectedMessages()})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "Cleanup.Handle") {
		t.Fatalf("unexpected: %+v methods=%v", findings, methods)
	}
}
func TestRowPolicyApplicationOnlyExplainsClassification(t *testing.T) {
	findings := ruleRowPolicyApplicationOnly(&AnalysisContext{RowPolicies: []generator.ResolvedRowPolicy{{Protection: schema.RowProtection{Table: "messages"}, EnforcementClass: "application_only"}}})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "cannot be lowered") {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestRowPolicyProofNeverClaimsLiveDualEnforcementWithoutInspection(t *testing.T) {
	policy := protectedMessages()[0]
	policy.EnforcementClass = "portable"
	policy.Protection.Rules = []schema.RowRule{{Key: "member"}}
	proofs := classifyRowPolicies(&AnalysisContext{RowPolicies: []generator.ResolvedRowPolicy{policy}}, nil)
	if len(proofs) != 1 || !strings.Contains(proofs[0].Classification, "live catalog uninspected") || strings.HasPrefix(proofs[0].Classification, "dual-enforced") {
		t.Fatalf("unexpected proof: %+v", proofs)
	}
	blocked := classifyRowPolicies(&AnalysisContext{RowPolicies: []generator.ResolvedRowPolicy{policy}}, []Finding{{Rule: "row_policy_context_missing"}})
	if blocked[0].Classification != "unproven" {
		t.Fatalf("unexpected blocked proof: %+v", blocked)
	}
}

func TestAllRowPolicyRuleIDsAreRegistered(t *testing.T) {
	rules := AllRules()
	for _, name := range []string{"row_policy_missing", "row_policy_unknown_identity", "row_policy_unlowerable", "row_policy_context_missing", "row_policy_bypass", "row_policy_projection_conflict", "rls_not_enabled", "rls_not_forced", "rls_runtime_bypass", "rls_manual_broadening", "rls_drift"} {
		if rules[name] == nil {
			t.Errorf("missing rule %s", name)
		}
	}
}

func TestLiveRLSRulesUseExplicitCatalogEvidence(t *testing.T) {
	ctx := &AnalysisContext{LiveRLS: []LiveRLSObservation{{Table: "messages", Enabled: false, Forced: false, RuntimeBypass: true, Drift: true, Detail: "predicate mismatch"}}}
	for name, rule := range map[string]Rule{"rls_not_enabled": ruleRLSNotEnabled, "rls_not_forced": ruleRLSNotForced, "rls_runtime_bypass": ruleRLSRuntimeBypass, "rls_drift": ruleRLSDrift} {
		findings := rule(ctx)
		if len(findings) != 1 || findings[0].Rule != name {
			t.Fatalf("%s: %+v", name, findings)
		}
	}
}

func TestLiveRLSRuntimeOwnerIsRejectedEvenWhenForced(t *testing.T) {
	findings := ruleRLSRuntimeBypass(&AnalysisContext{LiveRLS: []LiveRLSObservation{{Table: "messages", Enabled: true, Forced: true, RuntimeOwner: true}}})
	if len(findings) != 1 || findings[0].Rule != "rls_runtime_bypass" {
		t.Fatalf("forced runtime owner was not reported: %+v", findings)
	}
}

func TestRowPolicyProjectionConflictRequiresContradictoryVisibility(t *testing.T) {
	table := &schema.Table{Name: "messages", Columns: []*schema.Column{{Name: "secret", IsPublic: true, IsOwnerSees: true}}}
	findings := ruleRowPolicyProjectionConflict(&AnalysisContext{Tables: []*schema.Table{table}, RowPolicies: protectedMessages()})
	if len(findings) != 1 {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestRowPolicyProofDowngradesOnBypassOrIncompletePolicy(t *testing.T) {
	policy := generator.ResolvedRowPolicy{Protection: schema.RowProtection{Table: "messages", Rules: []schema.RowRule{{Key: "owner"}}}, EnforcementClass: "portable"}
	for _, rule := range []string{"row_policy_bypass", "row_policy_missing", "row_policy_unlowerable"} {
		proofs := classifyRowPolicies(&AnalysisContext{RowPolicies: []generator.ResolvedRowPolicy{policy}}, []Finding{{Rule: rule}})
		if proofs[0].Classification != "unproven" {
			t.Errorf("%s left classification %s", rule, proofs[0].Classification)
		}
	}
}
