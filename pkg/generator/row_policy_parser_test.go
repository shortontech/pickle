package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestParseAndResolveRowPolicies(t *testing.T) {
	dir := t.TempDir()
	src := `package policies
type MessageAccess_2026_07_16_120000 struct{ Policy }
func (p *MessageAccess_2026_07_16_120000) Up() {
	p.IdentityUUID("workspace_id")
	p.CreateRole("member")
	p.Protect("messages", func(rows *Rows) {
		rows.ExistingRowsAlreadyValid("table created empty")
		rows.Rule("member_workspace").ForRole("member").
			Select(Owner("workspace_id", Identity("workspace_id"))).
			Insert(Owner("workspace_id", Identity("workspace_id"))).
			Update(Existing(Owner("workspace_id", Identity("workspace_id"))), Proposed(Owner("workspace_id", Identity("workspace_id"))))
	})
}`
	path := filepath.Join(dir, "2026_07_16_120000_message_access.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ParseRowPolicyOps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(files[0].Operations) != 1 || len(files[0].Identities) != 1 {
		t.Fatalf("unexpected parse: %#v", files)
	}
	roles, err := ParsePolicyOps(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveRowPolicies(files, []*schema.Table{{Name: "messages", Columns: []*schema.Column{{Name: "workspace_id", Type: schema.UUID}}}}, StaticDeriveRoles(roles))
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].EnforcementClass != "portable" || resolved[0].PhysicalPlans["update"] != "update" {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
}

func TestParseAndResolveDillTenantRowPolicy(t *testing.T) {
	dir := t.TempDir()
	src := `package policies
type InventoryAccess_2026_07_16_120000 struct{ Policy }
func (p *InventoryAccess_2026_07_16_120000) Up() {
	p.IdentityInt64("user_id")
	p.IdentityInt64("organization_id")
	p.IdentityInt64s("allowed_company_ids")
	p.Protect("inventory_movements", func(rows *Rows) {
		rows.ExistingRowsAlreadyValid("tenant assignments verified")
		rows.Rule("tenant").ForAuthenticated().All(And(
			Equal(PolicyColumn("organization_id"), Identity("organization_id")),
			In(PolicyColumn("suborganization_id"), Identity("allowed_company_ids")),
		))
	})
}`
	if err := os.WriteFile(filepath.Join(dir, "2026_07_16_120000_inventory_access.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ParseRowPolicyOps(dir)
	if err != nil {
		t.Fatal(err)
	}
	tables := []*schema.Table{{Name: "inventory_movements", Columns: []*schema.Column{
		{Name: "organization_id", Type: schema.BigInteger},
		{Name: "suborganization_id", Type: schema.BigInteger},
	}}}
	resolved, err := ResolveRowPolicies(files, tables, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Identities["organization_id"] != schema.PolicyIdentityInt64 || resolved[0].Identities["allowed_company_ids"] != schema.PolicyIdentityInt64s {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
	got := resolved[0].Protection.Rules[0].Select
	if got == nil || got.Kind != schema.PredicateAnd || got.Children[1].Kind != schema.PredicateIn {
		t.Fatalf("membership predicate was not normalized: %#v", got)
	}
	if !got.Children[0].Children[0].HasColumnType || got.Children[0].Children[0].ColumnType != schema.BigInteger || !got.Children[1].Children[0].HasColumnType {
		t.Fatalf("resolved column types were not retained: %#v", got)
	}
}

func TestResolveRowPoliciesRejectsInvalidMembershipShapes(t *testing.T) {
	columns := []*schema.Column{{Name: "company_id", Type: schema.BigInteger}, {Name: "name", Type: schema.String}}
	identities := []schema.PolicyIdentityDefinition{{Name: "companies", Type: schema.PolicyIdentityInt64s}, {Name: "company", Type: schema.PolicyIdentityInt64}}
	for name, predicate := range map[string]schema.RowPredicate{
		"reversed": schema.In(schema.Identity("companies"), schema.PolicyColumn("company_id")),
		"scalar":   schema.In(schema.PolicyColumn("company_id"), schema.Identity("company")),
		"string":   schema.In(schema.PolicyColumn("name"), schema.Identity("companies")),
	} {
		t.Run(name, func(t *testing.T) {
			files := []ParsedRowPolicyFile{{PolicyID: "p1", Identities: identities, Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "items", ExistingRowsDecision: "verified", Rules: []schema.RowRule{{Key: "membership", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &predicate}}}}}}}
			if _, err := ResolveRowPolicies(files, []*schema.Table{{Name: "items", Columns: columns}}, nil); err == nil {
				t.Fatal("expected incompatible membership to fail")
			}
		})
	}
}

func TestResolveRowPoliciesRequiresExistingRowsDecision(t *testing.T) {
	allow := schema.Allow()
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "messages", Rules: []schema.RowRule{{Key: "read", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &allow}}}}}}}
	_, err := ResolveRowPolicies(files, []*schema.Table{{Name: "messages"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "ExistingRowsAlreadyValid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRowPoliciesRequiresApplicationOnlyAcknowledgement(t *testing.T) {
	files := []ParsedRowPolicyFile{{
		PolicyID:   "p1",
		Identities: []schema.PolicyIdentityDefinition{{Name: "workspace_id", Type: schema.PolicyIdentityUUID}, {Name: "user_id", Type: schema.PolicyIdentityUUID}},
		Operations: []schema.RowPolicyOperation{{
			Type: "protect",
			Protection: schema.RowProtection{
				Table:                "messages",
				ExistingRowsDecision: "table created empty",
				SubjectCombination:   schema.AnyOfSubjects,
				Rules: []schema.RowRule{{
					Key: "owner", Subject: schema.RowSubject{Kind: schema.SubjectAuthenticated},
					UpdateOld: &schema.RowPredicate{Kind: schema.PredicateAllow},
					UpdateNew: &schema.RowPredicate{Kind: schema.PredicateAllow},
				}},
			},
		}},
	}}
	_, err := ResolveRowPolicies(files, []*schema.Table{{Name: "messages", IsImmutable: true}}, nil)
	if err == nil || !strings.Contains(err.Error(), `AllowApplicationOnly("non_bijective_physical_plan")`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRowPoliciesRejectsAppendOnlyMutationRules(t *testing.T) {
	allow := schema.Allow()
	for _, mutation := range []string{"update", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			rule := schema.RowRule{Key: mutation, Subject: schema.RowSubject{Kind: schema.SubjectPublic}}
			if mutation == "update" {
				rule.UpdateOld, rule.UpdateNew = &allow, &allow
			} else {
				rule.Delete = &allow
			}
			files := []ParsedRowPolicyFile{{PolicyID: "p1", Operations: []schema.RowPolicyOperation{{
				Type: "protect", Protection: schema.RowProtection{Table: "events", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{rule}},
			}}}}
			_, err := ResolveRowPolicies(files, []*schema.Table{{Name: "events", IsAppendOnly: true}}, nil)
			if err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestResolveRowPoliciesRejectsUnknownIdentityAndColumn(t *testing.T) {
	pred := schema.RowPredicate{Kind: schema.PredicateEqual, Children: []schema.RowPredicate{{Kind: schema.PredicateColumn, Name: "tenant_id"}, {Kind: schema.PredicateIdentity, Name: "tenant_id"}}}
	files := []ParsedRowPolicyFile{{
		PolicyID: "p1",
		Operations: []schema.RowPolicyOperation{{
			Type: "protect",
			Protection: schema.RowProtection{Table: "messages", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{{
				Key: "owner", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &pred,
			}}},
		}},
	}}
	_, err := ResolveRowPolicies(files, []*schema.Table{{Name: "messages"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRowPoliciesResolvesExistingRowRelationship(t *testing.T) {
	pred := schema.Exists("memberships", schema.Equal(schema.PolicyColumn("workspace_id"), schema.Identity("workspace_id")))
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Identities: []schema.PolicyIdentityDefinition{{Name: "workspace_id", Type: schema.PolicyIdentityUUID}}, Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "users", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &pred}}}}}}}
	users := &schema.Table{Name: "users", Columns: []*schema.Column{{Name: "id", Type: schema.UUID, IsPrimaryKey: true}}}
	memberships := &schema.Table{Name: "memberships", Columns: []*schema.Column{{Name: "user_id", Type: schema.UUID, ForeignKeyTable: "users", ForeignKeyColumn: "id"}, {Name: "workspace_id", Type: schema.UUID}}}
	resolved, err := ResolveRowPolicies(files, []*schema.Table{users, memberships}, nil, []SchemaRelationship{{ParentTable: "users", ChildTable: "memberships"}})
	if err != nil {
		t.Fatal(err)
	}
	got := resolved[0].Protection.Rules[0].Select
	if got.RelatedTable != "memberships" || got.LocalColumn != "id" || got.ForeignColumn != "user_id" {
		t.Fatalf("unresolved relationship: %#v", got)
	}
}

func TestResolveRowPoliciesRejectsRelationshipInProposedRow(t *testing.T) {
	pred := schema.Exists("memberships", schema.Allow())
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "users", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Insert: &pred}}}}}}}
	users := &schema.Table{Name: "users", Columns: []*schema.Column{{Name: "id", IsPrimaryKey: true}}}
	memberships := &schema.Table{Name: "memberships", Columns: []*schema.Column{{Name: "user_id", ForeignKeyTable: "users"}}}
	_, err := ResolveRowPolicies(files, []*schema.Table{users, memberships}, nil, []SchemaRelationship{{ParentTable: "users", ChildTable: "memberships"}})
	if err == nil || !strings.Contains(err.Error(), "existing-row positions") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRowPoliciesFailsClosedOnUnsupportedSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026_07_16_120000_bad.go")
	src := `package policies
type Bad_2026_07_16_120000 struct{ Policy }
func (p *Bad_2026_07_16_120000) Up() { if true { p.Protect("messages", func(rows *Rows) {}) } }`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRowPolicyOps(dir); err == nil || !strings.Contains(err.Error(), "unsupported statement") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRowPoliciesAllowsDeclarativeRoleActionSlice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026_07_16_120000_role_actions.go")
	src := `package policies
type RoleActions_2026_07_16_120000 struct{ Policy }
func (p *RoleActions_2026_07_16_120000) Up() {
	write := []string{"people.view", "people.create"}
	p.AlterRole("member").Can(write...)
}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := ParseRowPolicyOps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("role-only policy produced row operations: %#v", rows)
	}
	roles, err := ParsePolicyOps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || len(roles[0].Ops) != 1 || strings.Join(roles[0].Ops[0].Actions, ",") != "people.view,people.create" {
		t.Fatalf("role action slice was not statically expanded: %#v", roles)
	}
}

func TestParseRowPoliciesRejectsComputedAssignment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026_07_16_120000_bad_assignment.go")
	src := `package policies
type Bad_2026_07_16_120000 struct{ Policy }
func (p *Bad_2026_07_16_120000) Up() {
	write := loadActions()
	p.AlterRole("member").Can(write...)
}`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRowPolicyOps(dir); err == nil || !strings.Contains(err.Error(), "unsupported assignment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveImmutableRowPolicyRequiresApplicationOnly(t *testing.T) {
	pred := schema.Allow()
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "messages", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{{Key: "read", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &pred}}}}}}}
	_, err := ResolveRowPolicies(files, []*schema.Table{{Name: "messages", IsImmutable: true}}, nil)
	if err == nil || !strings.Contains(err.Error(), `AllowApplicationOnly("non_bijective_physical_plan")`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRelationshipRejectsProtectedReferencedTable(t *testing.T) {
	exists := schema.Exists("memberships", schema.Equal(schema.PolicyColumn("workspace_id"), schema.Identity("workspace_id")))
	allow := schema.Allow()
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Identities: []schema.PolicyIdentityDefinition{{Name: "workspace_id", Type: schema.PolicyIdentityUUID}}, Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "users", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &exists}}}}, {Type: "protect", Protection: schema.RowProtection{Table: "memberships", ExistingRowsDecision: "table created empty", Rules: []schema.RowRule{{Key: "visible", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &allow}}}}}}}
	users := &schema.Table{Name: "users", Columns: []*schema.Column{{Name: "id", Type: schema.UUID, IsPrimaryKey: true}}}
	memberships := &schema.Table{Name: "memberships", Columns: []*schema.Column{{Name: "user_id", Type: schema.UUID, ForeignKeyTable: "users"}, {Name: "workspace_id", Type: schema.UUID}}}
	_, err := ResolveRowPolicies(files, []*schema.Table{users, memberships}, nil, []SchemaRelationship{{ParentTable: "users", ChildTable: "memberships"}})
	if err == nil || !strings.Contains(err.Error(), "evaluation privileges cannot be proven") {
		t.Fatalf("unexpected error: %v", err)
	}
}
