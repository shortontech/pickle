package picklemcp

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

func TestRenderRowPolicyIncludesNormalizedProofMetadata(t *testing.T) {
	predicate := schema.Equal(schema.PolicyColumn("workspace_id"), schema.Identity("workspace_id"))
	policy := generator.ResolvedRowPolicy{
		Protection:       schema.RowProtection{Table: "messages", SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{{Key: "workspace_member", Subject: schema.RowSubject{Kind: schema.SubjectAuthenticated}, Select: &predicate}}},
		Identities:       map[string]schema.PolicyIdentityType{"workspace_id": schema.PolicyIdentityUUID},
		SourcePolicies:   []string{"ProtectMessages_20260716000000"},
		EnforcementClass: "portable",
	}
	output := RenderRowPolicy(policy)
	for _, want := range []string{"messages", "unproven", "workspace_id (uuid)", "workspace_member", "equal(column(workspace_id), identity(workspace_id))", "ProtectMessages_20260716000000"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}

func TestRowPolicyApplicationOnlyClassificationIsHonest(t *testing.T) {
	policy := generator.ResolvedRowPolicy{EnforcementClass: "application_only"}
	if got := rowPolicyClassification(policy); got != "application_only" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderRowPolicyIncludesNumericMembership(t *testing.T) {
	predicate := schema.In(schema.PolicyColumn("suborganization_id"), schema.Identity("allowed_company_ids"))
	policy := generator.ResolvedRowPolicy{
		Protection:       schema.RowProtection{Table: "inventory_movements", Rules: []schema.RowRule{{Key: "allowed_company", Subject: schema.RowSubject{Kind: schema.SubjectAuthenticated}, Select: &predicate}}},
		Identities:       map[string]schema.PolicyIdentityType{"user_id": schema.PolicyIdentityInt64, "allowed_company_ids": schema.PolicyIdentityInt64s},
		EnforcementClass: "portable",
		PhysicalPlans:    map[string]string{"select": "select"},
	}
	output := RenderRowPolicy(policy)
	for _, want := range []string{"user_id (int64)", "allowed_company_ids (int64s)", "in(column(suborganization_id), identity(allowed_company_ids))"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}
