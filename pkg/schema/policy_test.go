package schema

import (
	"testing"
)

func TestPolicyCreateRole(t *testing.T) {
	p := &Policy{}
	p.CreateRole("admin").Name("Administrator").Manages().Can("users.create", "users.delete")

	if len(p.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(p.Operations))
	}

	op := p.Operations[0]
	if op.Type != "create" {
		t.Errorf("expected type 'create', got %q", op.Type)
	}
	if op.Role.Slug != "admin" {
		t.Errorf("expected slug 'admin', got %q", op.Role.Slug)
	}
	if op.Role.DisplayName != "Administrator" {
		t.Errorf("expected display name 'Administrator', got %q", op.Role.DisplayName)
	}
	if !op.Role.IsManages {
		t.Error("expected IsManages to be true")
	}
	if len(op.Role.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(op.Role.Actions))
	}
	if op.Role.Actions[0] != "users.create" || op.Role.Actions[1] != "users.delete" {
		t.Errorf("unexpected actions: %v", op.Role.Actions)
	}
}

func TestPolicyAlterRole(t *testing.T) {
	p := &Policy{}
	p.AlterRole("editor").Name("Content Editor").RemoveManages().Can("posts.publish").RevokeCan("posts.delete")

	op := p.Operations[0]
	if op.Type != "alter" {
		t.Errorf("expected type 'alter', got %q", op.Type)
	}
	if op.Role.DisplayName != "Content Editor" {
		t.Errorf("expected display name 'Content Editor', got %q", op.Role.DisplayName)
	}
	if !op.Role.RemoveManages {
		t.Error("expected RemoveManages to be true")
	}
	if len(op.Role.Actions) != 1 || op.Role.Actions[0] != "posts.publish" {
		t.Errorf("unexpected actions: %v", op.Role.Actions)
	}
	if len(op.Role.RevokeActions) != 1 || op.Role.RevokeActions[0] != "posts.delete" {
		t.Errorf("unexpected revoke actions: %v", op.Role.RevokeActions)
	}
}

func TestPolicyDropRole(t *testing.T) {
	p := &Policy{}
	p.DropRole("viewer")

	if len(p.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(p.Operations))
	}
	if p.Operations[0].Type != "drop" {
		t.Errorf("expected type 'drop', got %q", p.Operations[0].Type)
	}
	if p.Operations[0].Role.Slug != "viewer" {
		t.Errorf("expected slug 'viewer', got %q", p.Operations[0].Role.Slug)
	}
}

func TestPolicyDefault(t *testing.T) {
	p := &Policy{}
	p.CreateRole("member").Default()

	if !p.Operations[0].Role.IsDefault {
		t.Error("expected IsDefault to be true")
	}
}

func TestPolicyRemoveDefault(t *testing.T) {
	p := &Policy{}
	p.AlterRole("member").RemoveDefault()

	if !p.Operations[0].Role.RemoveDefault {
		t.Error("expected RemoveDefault to be true")
	}
}

func TestPolicyReset(t *testing.T) {
	p := &Policy{}
	p.CreateRole("admin")
	p.Reset()

	if len(p.Operations) != 0 {
		t.Errorf("expected 0 operations after reset, got %d", len(p.Operations))
	}
}

func TestPolicyTransactional(t *testing.T) {
	p := &Policy{}
	if !p.Transactional() {
		t.Error("expected Transactional to return true by default")
	}
}

func TestPolicyMultipleOperations(t *testing.T) {
	p := &Policy{}
	p.CreateRole("admin").Name("Administrator").Manages()
	p.CreateRole("editor").Name("Editor").Can("posts.create", "posts.edit")
	p.CreateRole("viewer").Name("Viewer").Default()

	if len(p.Operations) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(p.Operations))
	}
	if p.Operations[0].Role.Slug != "admin" {
		t.Errorf("expected first role 'admin', got %q", p.Operations[0].Role.Slug)
	}
	if p.Operations[1].Role.Slug != "editor" {
		t.Errorf("expected second role 'editor', got %q", p.Operations[1].Role.Slug)
	}
	if p.Operations[2].Role.Slug != "viewer" {
		t.Errorf("expected third role 'viewer', got %q", p.Operations[2].Role.Slug)
	}
}

func TestPolicyEmptySlugPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty slug")
		}
	}()
	p := &Policy{}
	p.CreateRole("")
}

func TestPolicyDropEmptySlugPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty slug")
		}
	}()
	p := &Policy{}
	p.DropRole("")
}
