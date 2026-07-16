package schema

import (
	"strings"
	"testing"
)

func TestRLSPolicyOperations(t *testing.T) {
	var m Migration
	m.EnableRLS("private.messages")
	m.ForceRLS("private.messages")
	m.CreateRLSPolicy("private.messages", "tenant_access", func(p *RLSPolicy) {
		p.For(RLSAll).To("dill_app").UsingExpression(SQLPredicate("tenant_id = current_setting('dill.tenant_id')::uuid")).WithSameCheck()
	})
	m.DropRLSPolicy("private.messages", "tenant_access")
	m.NoForceRLS("private.messages")
	m.DisableRLS("private.messages")

	if len(m.Operations) != 6 {
		t.Fatalf("got %d operations, want 6", len(m.Operations))
	}
	p := m.Operations[2].RLSPolicy
	if p == nil || p.Command != RLSAll || p.WithCheck != p.Using {
		t.Fatalf("unexpected policy: %#v", p)
	}
	q, err := postgresRLSSQL(m.Operations[2])
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE POLICY "tenant_access" ON "private"."messages" FOR ALL TO "dill_app" USING (tenant_id = current_setting('dill.tenant_id')::uuid) WITH CHECK (tenant_id = current_setting('dill.tenant_id')::uuid)`
	if q != want {
		t.Fatalf("SQL:\n%s\nwant:\n%s", q, want)
	}
}

func TestRLSPolicyRejectsInvalidCommandShape(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	var m Migration
	m.CreateRLSPolicy("messages", "read", func(p *RLSPolicy) {
		p.For(RLSSelect).UsingExpression(SQLPredicate("true")).WithCheckExpression(SQLPredicate("true"))
	})
}

func TestRLSPolicyRestrictiveDefenseInDepth(t *testing.T) {
	m := &Migration{}
	m.CreateRLSPolicy("messages", "message_archive", func(p *RLSPolicy) {
		p.For(RLSSelect).UsingExpression("archived_at IS NULL").RestrictiveDefenseInDepth()
	})
	sql, err := postgresRLSSQL(m.Operations[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `AS RESTRICTIVE FOR SELECT`) {
		t.Fatalf("expected restrictive policy SQL, got %s", sql)
	}
}

func TestRLSPolicyGeneratedNamespaceIsReserved(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected reserved generated policy name to panic")
		}
	}()
	(&Migration{}).CreateRLSPolicy("messages", "pickle_messages_select_deadbeef", func(p *RLSPolicy) {
		p.For(RLSSelect).UsingExpression("true")
	})
}
