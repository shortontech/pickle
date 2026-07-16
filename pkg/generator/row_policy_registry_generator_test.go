package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestGenerateRowPolicyRegistryIncludesManagedDDL(t *testing.T) {
	src, err := GenerateRowPolicyRegistry("policies", []PostgresRowPolicyPlan{{Table: "private.messages", Enable: true, Force: true, Policies: []GeneratedPostgresRowPolicy{{Name: "pickle_messages_select_123", Table: "private.messages", Command: schema.RLSSelect, Using: "TRUE"}}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{"GeneratedRowPolicyDDL", `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY`, `DROP POLICY IF EXISTS`, `CREATE POLICY`, `COMMENT ON POLICY`, `GeneratedRowPolicyDesired`, `GeneratedRowPolicyCatalog`, `GeneratedRowPolicyFingerprintValue`, `private.messages`} {
		if !strings.Contains(text, want) {
			t.Errorf("generated source missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateRowPolicyRuntimeRegistryUsesResolvedIR(t *testing.T) {
	predicate := schema.RowPredicate{Kind: schema.PredicateEqual, Children: []schema.RowPredicate{{Kind: schema.PredicateColumn, Name: "workspace_id"}, {Kind: schema.PredicateIdentity, Name: "workspace_id"}}}
	resolved := ResolvedRowPolicy{
		Protection: schema.RowProtection{
			Table: "messages", SubjectCombination: schema.AnyOfSubjects,
			Rules: []schema.RowRule{{Key: "owner", Subject: schema.RowSubject{Kind: schema.SubjectAuthenticated}, Select: &predicate}},
		},
		EnforcementClass: "portable",
		Identities:       map[string]schema.PolicyIdentityType{"workspace_id": schema.PolicyIdentityUUID},
	}
	src, err := GenerateRowPolicyRuntimeRegistry("models", []ResolvedRowPolicy{resolved})
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{"registerRowPolicyRuntime", "messages", "workspace_id", "authenticated", "equal"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateRowPolicyRegistryExplicitlyDisablesUnprotectedTable(t *testing.T) {
	src, err := GenerateRowPolicyRegistry("policies", nil, []string{"private.messages"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	for _, want := range []string{`ALTER TABLE \"private\".\"messages\" NO FORCE ROW LEVEL SECURITY`, `ALTER TABLE \"private\".\"messages\" DISABLE ROW LEVEL SECURITY`, `"private.messages": {}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
