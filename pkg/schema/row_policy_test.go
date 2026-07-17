package schema

import "testing"

func TestPolicyProtectBuildsNormalizedRowRules(t *testing.T) {
	p := &Policy{}
	p.Protect("messages", func(rows *Rows) {
		rows.ExistingRowsAlreadyValid("table created empty")
		rows.Rule("member_workspace").ForRole("member").
			Select(Owner("workspace_id", Identity("workspace_id"))).
			Insert(Owner("workspace_id", Identity("workspace_id"))).
			Update(Existing(Owner("workspace_id", Identity("workspace_id"))), Proposed(Owner("workspace_id", Identity("workspace_id"))))
	})
	ops := p.GetRowOperations()
	if len(ops) != 1 {
		t.Fatalf("got %d operations", len(ops))
	}
	got := ops[0].Protection
	if got.Table != "messages" || got.SubjectCombination != AnyOfSubjects || len(got.Rules) != 1 {
		t.Fatalf("unexpected protection: %#v", got)
	}
	if got.Rules[0].UpdateOld == nil || got.Rules[0].UpdateNew == nil {
		t.Fatal("missing update positions")
	}
}

func TestPolicyProtectRejectsDuplicateStableKeys(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	p := &Policy{}
	p.Protect("messages", func(rows *Rows) {
		rows.ExistingRowsAlreadyValid("table created empty")
		rows.Rule("same").ForPublic().Select(Allow())
		rows.Rule("same").ForAuthenticated().Select(Allow())
	})
}

func TestPolicyResetClearsRowOperations(t *testing.T) {
	p := &Policy{}
	p.IdentityUUID("workspace_id")
	p.Unprotect("messages")
	p.Reset()
	if len(p.GetRowOperations()) != 0 {
		t.Fatal("row operations not cleared")
	}
	if len(p.GetIdentityDefinitions()) != 0 {
		t.Fatal("identity definitions not cleared")
	}
}

func TestPolicyBuildsNumericMembershipIdentity(t *testing.T) {
	p := &Policy{}
	p.IdentityInt64("organization_id")
	p.IdentityInt64s("allowed_company_ids")
	p.Protect("movements", func(rows *Rows) {
		rows.ExistingRowsAlreadyValid("empty")
		rows.Rule("tenant").ForPublic().Select(And(
			Equal(PolicyColumn("organization_id"), Identity("organization_id")),
			In(PolicyColumn("suborganization_id"), Identity("allowed_company_ids")),
		))
	})
	identities := p.GetIdentityDefinitions()
	if len(identities) != 2 || identities[0].Type != PolicyIdentityInt64 || identities[1].Type != PolicyIdentityInt64s {
		t.Fatalf("unexpected identities: %#v", identities)
	}
	if got := p.GetRowOperations()[0].Protection.Rules[0].Select.Children[1].Kind; got != PredicateIn {
		t.Fatalf("predicate kind=%s", got)
	}
}
