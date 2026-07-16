package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
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
