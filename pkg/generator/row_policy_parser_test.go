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

func TestResolveRowPoliciesRequiresApplicationOnlyAcknowledgement(t *testing.T) {
	files := []ParsedRowPolicyFile{{
		PolicyID:   "p1",
		Identities: []schema.PolicyIdentityDefinition{{Name: "workspace_id", Type: schema.PolicyIdentityUUID}, {Name: "user_id", Type: schema.PolicyIdentityUUID}},
		Operations: []schema.RowPolicyOperation{{
			Type: "protect",
			Protection: schema.RowProtection{
				Table:              "messages",
				SubjectCombination: schema.AnyOfSubjects,
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

func TestResolveRowPoliciesRejectsUnknownIdentityAndColumn(t *testing.T) {
	pred := schema.RowPredicate{Kind: schema.PredicateEqual, Children: []schema.RowPredicate{{Kind: schema.PredicateColumn, Name: "tenant_id"}, {Kind: schema.PredicateIdentity, Name: "tenant_id"}}}
	files := []ParsedRowPolicyFile{{
		PolicyID: "p1",
		Operations: []schema.RowPolicyOperation{{
			Type: "protect",
			Protection: schema.RowProtection{Table: "messages", Rules: []schema.RowRule{{
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
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Identities: []schema.PolicyIdentityDefinition{{Name: "workspace_id", Type: schema.PolicyIdentityUUID}}, Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "users", Rules: []schema.RowRule{{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &pred}}}}}}}
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
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "users", Rules: []schema.RowRule{{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Insert: &pred}}}}}}}
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
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "messages", Rules: []schema.RowRule{{Key: "read", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &pred}}}}}}}
	_, err := ResolveRowPolicies(files, []*schema.Table{{Name: "messages", IsImmutable: true}}, nil)
	if err == nil || !strings.Contains(err.Error(), `AllowApplicationOnly("non_bijective_physical_plan")`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveRelationshipRejectsProtectedReferencedTable(t *testing.T) {
	exists := schema.Exists("memberships", schema.Equal(schema.PolicyColumn("workspace_id"), schema.Identity("workspace_id")))
	allow := schema.Allow()
	files := []ParsedRowPolicyFile{{PolicyID: "p1", Identities: []schema.PolicyIdentityDefinition{{Name: "workspace_id", Type: schema.PolicyIdentityUUID}}, Operations: []schema.RowPolicyOperation{{Type: "protect", Protection: schema.RowProtection{Table: "users", Rules: []schema.RowRule{{Key: "member", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &exists}}}}, {Type: "protect", Protection: schema.RowProtection{Table: "memberships", Rules: []schema.RowRule{{Key: "visible", Subject: schema.RowSubject{Kind: schema.SubjectPublic}, Select: &allow}}}}}}}
	users := &schema.Table{Name: "users", Columns: []*schema.Column{{Name: "id", Type: schema.UUID, IsPrimaryKey: true}}}
	memberships := &schema.Table{Name: "memberships", Columns: []*schema.Column{{Name: "user_id", Type: schema.UUID, ForeignKeyTable: "users"}, {Name: "workspace_id", Type: schema.UUID}}}
	_, err := ResolveRowPolicies(files, []*schema.Table{users, memberships}, nil, []SchemaRelationship{{ParentTable: "users", ChildTable: "memberships"}})
	if err == nil || !strings.Contains(err.Error(), "evaluation privileges cannot be proven") {
		t.Fatalf("unexpected error: %v", err)
	}
}
