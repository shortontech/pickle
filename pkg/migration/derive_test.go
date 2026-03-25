//go:build ignore

package migration

import (
	"testing"
)

// testPolicy is a concrete policy for testing.
type testPolicy struct {
	Policy
	up   func(*Policy)
	down func(*Policy)
}

func (p *testPolicy) Up()   { p.up(&p.Policy) }
func (p *testPolicy) Down() { p.down(&p.Policy) }

func TestDeriveRolesBasic(t *testing.T) {
	entries := []PolicyEntry{
		{
			ID: "2026_03_01_100000_create_roles",
			Policy: &testPolicy{
				up: func(p *Policy) {
					p.CreateRole("admin").Name("Administrator").Manages().Can("users.create", "users.delete")
					p.CreateRole("editor").Name("Editor").Can("posts.create", "posts.edit")
					p.CreateRole("viewer").Name("Viewer").Default()
				},
				down: func(p *Policy) {
					p.DropRole("viewer")
					p.DropRole("editor")
					p.DropRole("admin")
				},
			},
		},
	}

	roles := DeriveRoles(entries)
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(roles))
	}

	admin := roles[0]
	if admin.Slug != "admin" || admin.DisplayName != "Administrator" || !admin.IsManages {
		t.Errorf("admin: %+v", admin)
	}
	if len(admin.Actions) != 2 {
		t.Errorf("admin actions: %v", admin.Actions)
	}
	if admin.BirthTimestamp != "2026_03_01_100000_create_roles" {
		t.Errorf("admin birth timestamp: %q", admin.BirthTimestamp)
	}

	viewer := roles[2]
	if !viewer.IsDefault {
		t.Error("viewer should be default")
	}
}

func TestDeriveRolesAlter(t *testing.T) {
	entries := []PolicyEntry{
		{
			ID: "001",
			Policy: &testPolicy{
				up: func(p *Policy) {
					p.CreateRole("editor").Name("Editor").Can("posts.create", "posts.delete")
				},
				down: func(p *Policy) { p.DropRole("editor") },
			},
		},
		{
			ID: "002",
			Policy: &testPolicy{
				up: func(p *Policy) {
					p.AlterRole("editor").Name("Content Editor").Can("posts.publish").RevokeCan("posts.delete")
				},
				down: func(p *Policy) {
					p.AlterRole("editor").Name("Editor").RevokeCan("posts.publish").Can("posts.delete")
				},
			},
		},
	}

	roles := DeriveRoles(entries)
	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}

	editor := roles[0]
	if editor.DisplayName != "Content Editor" {
		t.Errorf("expected 'Content Editor', got %q", editor.DisplayName)
	}
	if len(editor.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d: %v", len(editor.Actions), editor.Actions)
	}
	// Should have posts.create and posts.publish (posts.delete was revoked)
	hasCreate, hasPublish, hasDelete := false, false, false
	for _, a := range editor.Actions {
		switch a {
		case "posts.create":
			hasCreate = true
		case "posts.publish":
			hasPublish = true
		case "posts.delete":
			hasDelete = true
		}
	}
	if !hasCreate || !hasPublish || hasDelete {
		t.Errorf("unexpected actions: %v", editor.Actions)
	}
}

func TestDeriveRolesDrop(t *testing.T) {
	entries := []PolicyEntry{
		{
			ID: "001",
			Policy: &testPolicy{
				up:   func(p *Policy) { p.CreateRole("temp").Name("Temporary") },
				down: func(p *Policy) { p.DropRole("temp") },
			},
		},
		{
			ID: "002",
			Policy: &testPolicy{
				up:   func(p *Policy) { p.DropRole("temp") },
				down: func(p *Policy) { p.CreateRole("temp").Name("Temporary") },
			},
		},
	}

	roles := DeriveRoles(entries)
	if len(roles) != 0 {
		t.Errorf("expected 0 roles after drop, got %d", len(roles))
	}
}

func TestDeriveRolesDropAndRecreate(t *testing.T) {
	entries := []PolicyEntry{
		{
			ID: "001",
			Policy: &testPolicy{
				up:   func(p *Policy) { p.CreateRole("temp").Name("V1") },
				down: func(p *Policy) { p.DropRole("temp") },
			},
		},
		{
			ID: "002",
			Policy: &testPolicy{
				up:   func(p *Policy) { p.DropRole("temp") },
				down: func(p *Policy) { p.CreateRole("temp").Name("V1") },
			},
		},
		{
			ID: "003",
			Policy: &testPolicy{
				up:   func(p *Policy) { p.CreateRole("temp").Name("V2") },
				down: func(p *Policy) { p.DropRole("temp") },
			},
		},
	}

	roles := DeriveRoles(entries)
	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}
	if roles[0].DisplayName != "V2" {
		t.Errorf("expected 'V2', got %q", roles[0].DisplayName)
	}
	if roles[0].BirthTimestamp != "003" {
		t.Errorf("expected birth timestamp '003', got %q", roles[0].BirthTimestamp)
	}
}

// testGraphQLPolicy is a concrete GraphQL policy for testing.
type testGraphQLPolicy struct {
	GraphQLPolicy
	up   func(*GraphQLPolicy)
	down func(*GraphQLPolicy)
}

func (p *testGraphQLPolicy) Up()   { p.up(&p.GraphQLPolicy) }
func (p *testGraphQLPolicy) Down() { p.down(&p.GraphQLPolicy) }

func TestDeriveGraphQLStateBasic(t *testing.T) {
	entries := []GraphQLPolicyEntry{
		{
			ID: "001",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.Expose("User", func(e *ExposeBuilder) { e.List(); e.Show() })
				},
				down: func(p *GraphQLPolicy) { p.Unexpose("User") },
			},
		},
	}

	state := DeriveGraphQLState(entries)
	if len(state.Exposures) != 1 {
		t.Fatalf("expected 1 exposure, got %d", len(state.Exposures))
	}
	if state.Exposures[0].Model != "User" {
		t.Errorf("expected model 'User', got %q", state.Exposures[0].Model)
	}
	if len(state.Exposures[0].Operations) != 2 {
		t.Errorf("expected 2 ops, got %v", state.Exposures[0].Operations)
	}
}

func TestDeriveGraphQLStateAll(t *testing.T) {
	entries := []GraphQLPolicyEntry{
		{
			ID: "001",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.Expose("User", func(e *ExposeBuilder) { e.All() })
				},
				down: func(p *GraphQLPolicy) { p.Unexpose("User") },
			},
		},
	}

	state := DeriveGraphQLState(entries)
	if len(state.Exposures[0].Operations) != 5 {
		t.Errorf("expected 5 ops from All(), got %v", state.Exposures[0].Operations)
	}
}

func TestDeriveGraphQLStateAlterExpose(t *testing.T) {
	entries := []GraphQLPolicyEntry{
		{
			ID: "001",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.Expose("User", func(e *ExposeBuilder) { e.All() })
				},
				down: func(p *GraphQLPolicy) { p.Unexpose("User") },
			},
		},
		{
			ID: "002",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.AlterExpose("User", func(e *ExposeBuilder) { e.RemoveDelete() })
				},
				down: func(p *GraphQLPolicy) {
					p.AlterExpose("User", func(e *ExposeBuilder) { e.Delete() })
				},
			},
		},
	}

	state := DeriveGraphQLState(entries)
	ops := state.Exposures[0].Operations
	if len(ops) != 4 {
		t.Fatalf("expected 4 ops, got %v", ops)
	}
	for _, op := range ops {
		if op == "delete" {
			t.Error("delete should have been removed")
		}
	}
}

func TestDeriveGraphQLStateUnexpose(t *testing.T) {
	entries := []GraphQLPolicyEntry{
		{
			ID: "001",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.Expose("User", func(e *ExposeBuilder) { e.All() })
				},
				down: func(p *GraphQLPolicy) { p.Unexpose("User") },
			},
		},
		{
			ID: "002",
			Policy: &testGraphQLPolicy{
				up:   func(p *GraphQLPolicy) { p.Unexpose("User") },
				down: func(p *GraphQLPolicy) { p.Expose("User", func(e *ExposeBuilder) { e.All() }) },
			},
		},
	}

	state := DeriveGraphQLState(entries)
	if len(state.Exposures) != 0 {
		t.Errorf("expected 0 exposures after unexpose, got %d", len(state.Exposures))
	}
}

func TestDeriveGraphQLStateActions(t *testing.T) {
	entries := []GraphQLPolicyEntry{
		{
			ID: "001",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.ControllerAction("banUser", nil)
					p.ControllerAction("resetPassword", nil)
				},
				down: func(p *GraphQLPolicy) {
					p.RemoveAction("resetPassword")
					p.RemoveAction("banUser")
				},
			},
		},
		{
			ID: "002",
			Policy: &testGraphQLPolicy{
				up:   func(p *GraphQLPolicy) { p.RemoveAction("banUser") },
				down: func(p *GraphQLPolicy) { p.ControllerAction("banUser", nil) },
			},
		},
	}

	state := DeriveGraphQLState(entries)
	if len(state.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(state.Actions))
	}
	if state.Actions[0].Name != "resetPassword" {
		t.Errorf("expected 'resetPassword', got %q", state.Actions[0].Name)
	}
}

func TestDeriveGraphQLStateIncremental(t *testing.T) {
	entries := []GraphQLPolicyEntry{
		{
			ID: "001",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.Expose("Post", func(e *ExposeBuilder) { e.List(); e.Show() })
				},
				down: func(p *GraphQLPolicy) { p.Unexpose("Post") },
			},
		},
		{
			ID: "002",
			Policy: &testGraphQLPolicy{
				up: func(p *GraphQLPolicy) {
					p.AlterExpose("Post", func(e *ExposeBuilder) { e.Create(); e.Update() })
				},
				down: func(p *GraphQLPolicy) {
					p.AlterExpose("Post", func(e *ExposeBuilder) { e.RemoveCreate(); e.RemoveUpdate() })
				},
			},
		},
	}

	state := DeriveGraphQLState(entries)
	if len(state.Exposures) != 1 {
		t.Fatalf("expected 1 exposure, got %d", len(state.Exposures))
	}
	ops := state.Exposures[0].Operations
	if len(ops) != 4 {
		t.Errorf("expected 4 ops (list, show, create, update), got %v", ops)
	}
}
