package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestLowerPostgresRowPoliciesAggregatesSubjects(t *testing.T) {
	owner := schema.RowPredicate{Kind: schema.PredicateEqual, Children: []schema.RowPredicate{{Kind: schema.PredicateColumn, Name: "workspace_id"}, {Kind: schema.PredicateIdentity, Name: "workspace_id"}}}
	allow := schema.RowPredicate{Kind: schema.PredicateAllow}
	resolved := ResolvedRowPolicy{Protection: schema.RowProtection{Table: "messages", SubjectCombination: schema.AnyOfSubjects, Rules: []schema.RowRule{
		{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectRole, Name: "member"}, Select: &owner},
		{Key: "admin", Subject: schema.RowSubject{Kind: schema.SubjectRole, Name: "admin"}, Select: &allow},
	}}, EnforcementClass: "portable", PhysicalPlans: map[string]string{"select": "select"}, Identities: map[string]schema.PolicyIdentityType{"workspace_id": schema.PolicyIdentityUUID}}
	plans, err := LowerPostgresRowPolicies([]ResolvedRowPolicy{resolved})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Policies) != 1 {
		t.Fatalf("unexpected plans: %#v", plans)
	}
	using := plans[0].Policies[0].Using
	for _, want := range []string{"pickle_identity_has_role('member')", `"workspace_id"`, "pickle_identity_uuid('workspace_id')", " OR ", "pickle_identity_has_role('admin')"} {
		if !strings.Contains(using, want) {
			t.Errorf("missing %q: %s", want, using)
		}
	}
}

func TestLowerPostgresRowPoliciesSkipsApplicationOnly(t *testing.T) {
	plans, err := LowerPostgresRowPolicies([]ResolvedRowPolicy{{EnforcementClass: "application_only"}})
	if err != nil || len(plans) != 0 {
		t.Fatalf("plans=%#v err=%v", plans, err)
	}
}
func TestGeneratedRowPolicyNameFitsPostgres(t *testing.T) {
	name := generatedRowPolicyName(strings.Repeat("long_table_", 10), "select")
	if len(name) > 63 || !strings.HasPrefix(name, "pickle_") {
		t.Fatalf("bad name %q", name)
	}
}
