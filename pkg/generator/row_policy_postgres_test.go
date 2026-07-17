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

func TestPostgresRowPredicateLowersResolvedRelationship(t *testing.T) {
	predicate := schema.Exists("memberships", schema.Equal(schema.PolicyColumn("workspace_id"), schema.Identity("workspace_id")))
	predicate.RelatedTable, predicate.LocalColumn, predicate.ForeignColumn = "memberships", "id", "user_id"
	sql, err := postgresRowPredicate(predicate, map[string]schema.PolicyIdentityType{"workspace_id": schema.PolicyIdentityUUID})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`EXISTS (SELECT 1 FROM "memberships" pickle_rel`, `pickle_rel."user_id" = "id"`, `pickle_rel."workspace_id"`, `pickle_identity_uuid('workspace_id')`} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in %s", want, sql)
		}
	}
}

func TestPostgresRowPredicateLowersInt64AndMembership(t *testing.T) {
	predicate := schema.And(
		schema.Equal(schema.PolicyColumn("organization_id"), schema.Identity("organization_id")),
		schema.In(schema.PolicyColumn("suborganization_id"), schema.Identity("allowed_company_ids")),
	)
	sql, err := postgresRowPredicate(predicate, map[string]schema.PolicyIdentityType{
		"organization_id":     schema.PolicyIdentityInt64,
		"allowed_company_ids": schema.PolicyIdentityInt64s,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"organization_id" = pickle_identity_int64('organization_id')`, `"suborganization_id" = ANY(pickle_identity_int64s('allowed_company_ids'))`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in %s", want, sql)
		}
	}
	helpers := strings.Join(PostgresPolicyIdentityHelpers(), "\n")
	for _, want := range []string{"pickle_identity_int64(identity_name text)", "pickle_identity_int64s(identity_name text)", "json_array_length(parsed) > 1024"} {
		if !strings.Contains(helpers, want) {
			t.Fatalf("missing helper %q", want)
		}
	}
}

func TestNumericRowPolicyFingerprintIncludesIdentityPredicateAndColumnType(t *testing.T) {
	resolved := func(columnType schema.ColumnType, membership bool) ResolvedRowPolicy {
		column := schema.PolicyColumn("company_id")
		column.ColumnType, column.HasColumnType = columnType, true
		identityType := schema.PolicyIdentityInt64
		predicate := schema.Equal(column, schema.Identity("companies"))
		if membership {
			identityType = schema.PolicyIdentityInt64s
			predicate = schema.In(column, schema.Identity("companies"))
		}
		return ResolvedRowPolicy{
			Protection:       schema.RowProtection{Table: "items", Rules: []schema.RowRule{{Key: "companies", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate}}},
			EnforcementClass: "portable", Identities: map[string]schema.PolicyIdentityType{"companies": identityType}, PhysicalPlans: map[string]string{"select": "select"},
		}
	}
	fingerprint := func(policy ResolvedRowPolicy) string {
		plans, err := LowerPostgresRowPolicies([]ResolvedRowPolicy{policy})
		if err != nil {
			t.Fatal(err)
		}
		return GeneratedRowPolicyFingerprint(plans)
	}
	scalarInteger := fingerprint(resolved(schema.Integer, false))
	setInteger := fingerprint(resolved(schema.Integer, true))
	setBigInteger := fingerprint(resolved(schema.BigInteger, true))
	if scalarInteger == setInteger || setInteger == setBigInteger || scalarInteger == setBigInteger {
		t.Fatalf("fingerprints did not preserve normalized type transitions: scalar=%s set-int=%s set-bigint=%s", scalarInteger, setInteger, setBigInteger)
	}
}
