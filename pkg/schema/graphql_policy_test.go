package schema

import (
	"testing"
)

func TestGraphQLPolicyExpose(t *testing.T) {
	p := &GraphQLPolicy{}
	p.Expose("User", func(e *ExposeBuilder) {
		e.List()
		e.Show()
	})

	if len(p.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(p.Operations))
	}

	op := p.Operations[0]
	if op.Type != "expose" {
		t.Errorf("expected type 'expose', got %q", op.Type)
	}
	if op.Model != "User" {
		t.Errorf("expected model 'User', got %q", op.Model)
	}
	if len(op.Ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(op.Ops))
	}
	if op.Ops[0].Type != "list" || op.Ops[1].Type != "show" {
		t.Errorf("unexpected ops: %v", op.Ops)
	}
}

func TestGraphQLPolicyExposeAll(t *testing.T) {
	p := &GraphQLPolicy{}
	p.Expose("User", func(e *ExposeBuilder) {
		e.All()
	})

	ops := p.Operations[0].Ops
	if len(ops) != 5 {
		t.Fatalf("expected 5 ops from All(), got %d", len(ops))
	}
	expected := []string{"list", "show", "create", "update", "delete"}
	for i, e := range expected {
		if ops[i].Type != e {
			t.Errorf("ops[%d]: expected %q, got %q", i, e, ops[i].Type)
		}
	}
}

func TestGraphQLPolicyAlterExpose(t *testing.T) {
	p := &GraphQLPolicy{}
	p.AlterExpose("User", func(e *ExposeBuilder) {
		e.RemoveDelete()
	})

	op := p.Operations[0]
	if op.Type != "alter_expose" {
		t.Errorf("expected type 'alter_expose', got %q", op.Type)
	}
	if len(op.Ops) != 1 || op.Ops[0].Type != "remove_delete" {
		t.Errorf("unexpected ops: %v", op.Ops)
	}
}

func TestGraphQLPolicyUnexpose(t *testing.T) {
	p := &GraphQLPolicy{}
	p.Unexpose("User")

	if len(p.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(p.Operations))
	}
	if p.Operations[0].Type != "unexpose" {
		t.Errorf("expected type 'unexpose', got %q", p.Operations[0].Type)
	}
	if p.Operations[0].Model != "User" {
		t.Errorf("expected model 'User', got %q", p.Operations[0].Model)
	}
}

func TestGraphQLPolicyControllerAction(t *testing.T) {
	p := &GraphQLPolicy{}
	p.ControllerAction("banUser", nil)

	op := p.Operations[0]
	if op.Type != "controller_action" {
		t.Errorf("expected type 'controller_action', got %q", op.Type)
	}
	if op.Action.Name != "banUser" {
		t.Errorf("expected action name 'banUser', got %q", op.Action.Name)
	}
}

func TestGraphQLPolicyRemoveAction(t *testing.T) {
	p := &GraphQLPolicy{}
	p.RemoveAction("banUser")

	op := p.Operations[0]
	if op.Type != "remove_action" {
		t.Errorf("expected type 'remove_action', got %q", op.Type)
	}
	if op.Action.Name != "banUser" {
		t.Errorf("expected action name 'banUser', got %q", op.Action.Name)
	}
}

func TestGraphQLPolicyReset(t *testing.T) {
	p := &GraphQLPolicy{}
	p.Expose("User", func(e *ExposeBuilder) { e.All() })
	p.Reset()

	if len(p.Operations) != 0 {
		t.Errorf("expected 0 operations after reset, got %d", len(p.Operations))
	}
}

func TestGraphQLPolicyTransactional(t *testing.T) {
	p := &GraphQLPolicy{}
	if !p.Transactional() {
		t.Error("expected Transactional to return true by default")
	}
}

func TestGraphQLPolicyExposeEmptyModelPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty model")
		}
	}()
	p := &GraphQLPolicy{}
	p.Expose("", func(e *ExposeBuilder) { e.All() })
}

func TestGraphQLPolicyRemoveOperations(t *testing.T) {
	p := &GraphQLPolicy{}
	p.AlterExpose("Post", func(e *ExposeBuilder) {
		e.RemoveList()
		e.RemoveShow()
		e.RemoveCreate()
		e.RemoveUpdate()
		e.RemoveDelete()
	})

	ops := p.Operations[0].Ops
	if len(ops) != 5 {
		t.Fatalf("expected 5 remove ops, got %d", len(ops))
	}
	expected := []string{"remove_list", "remove_show", "remove_create", "remove_update", "remove_delete"}
	for i, e := range expected {
		if ops[i].Type != e {
			t.Errorf("ops[%d]: expected %q, got %q", i, e, ops[i].Type)
		}
	}
}

func TestGraphQLPolicyMultipleExposures(t *testing.T) {
	p := &GraphQLPolicy{}
	p.Expose("User", func(e *ExposeBuilder) { e.List(); e.Show() })
	p.Expose("Post", func(e *ExposeBuilder) { e.All() })
	p.ControllerAction("banUser", nil)

	if len(p.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(p.Operations))
	}
}
