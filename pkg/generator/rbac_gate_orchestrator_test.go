package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePolicyOps_Basic(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "2026_03_01_100000_create_roles.go", `package policies

type CreateRoles_2026_03_01_100000 struct{ Policy }

func (m *CreateRoles_2026_03_01_100000) Up() {
	m.CreateRole("admin").Name("Administrator").Manages().Can("users.create", "users.delete")
	m.CreateRole("moderator").Name("Moderator").Can("BanUser", "MuteUser")
	m.CreateRole("viewer").Name("Viewer").Default()
}

func (m *CreateRoles_2026_03_01_100000) Down() {
	m.DropRole("viewer")
	m.DropRole("moderator")
	m.DropRole("admin")
}
`)

	policies, err := ParsePolicyOps(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}

	ops := policies[0].Ops
	if len(ops) != 3 {
		t.Fatalf("expected 3 ops, got %d", len(ops))
	}

	// admin: create, manages, Can("users.create", "users.delete")
	admin := ops[0]
	if admin.Type != "create" || admin.Slug != "admin" || !admin.IsManages {
		t.Errorf("admin op: %+v", admin)
	}
	if len(admin.Actions) != 2 || admin.Actions[0] != "users.create" || admin.Actions[1] != "users.delete" {
		t.Errorf("admin actions: %v", admin.Actions)
	}

	// moderator: create, Can("BanUser", "MuteUser")
	mod := ops[1]
	if mod.Type != "create" || mod.Slug != "moderator" {
		t.Errorf("moderator op: %+v", mod)
	}
	if len(mod.Actions) != 2 || mod.Actions[0] != "BanUser" || mod.Actions[1] != "MuteUser" {
		t.Errorf("moderator actions: %v", mod.Actions)
	}

	// viewer: create, default
	viewer := ops[2]
	if viewer.Type != "create" || viewer.Slug != "viewer" || !viewer.IsDefault {
		t.Errorf("viewer op: %+v", viewer)
	}
}

func TestParsePolicyOps_AlterAndRevoke(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "2026_03_01_100000_create_roles.go", `package policies

type CreateRoles_2026_03_01_100000 struct{ Policy }

func (m *CreateRoles_2026_03_01_100000) Up() {
	m.CreateRole("moderator").Name("Moderator").Can("BanUser", "MuteUser")
}

func (m *CreateRoles_2026_03_01_100000) Down() {}
`)
	writeTestFile(t, dir, "2026_03_02_100000_alter_moderator.go", `package policies

type AlterModerator_2026_03_02_100000 struct{ Policy }

func (m *AlterModerator_2026_03_02_100000) Up() {
	m.AlterRole("moderator").RevokeCan("MuteUser").Can("WarnUser")
}

func (m *AlterModerator_2026_03_02_100000) Down() {}
`)

	policies, err := ParsePolicyOps(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}

	roles := StaticDeriveRoles(policies)
	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}

	mod := roles[0]
	if mod.Slug != "moderator" {
		t.Errorf("expected moderator, got %q", mod.Slug)
	}

	// Should have BanUser and WarnUser (MuteUser was revoked)
	hasActions := map[string]bool{}
	for _, a := range mod.Actions {
		hasActions[a] = true
	}
	if !hasActions["BanUser"] || !hasActions["WarnUser"] || hasActions["MuteUser"] {
		t.Errorf("unexpected actions: %v", mod.Actions)
	}
}

func TestStaticDeriveRoles_BirthTimestamp(t *testing.T) {
	policies := []StaticPolicyOps{
		{
			PolicyID: "001",
			Ops: []StaticRoleOp{
				{Type: "create", Slug: "temp", DisplayName: "V1", Actions: []string{"OldAction"}},
			},
		},
		{
			PolicyID: "002",
			Ops: []StaticRoleOp{
				{Type: "drop", Slug: "temp"},
			},
		},
		{
			PolicyID: "003",
			Ops: []StaticRoleOp{
				{Type: "create", Slug: "temp", DisplayName: "V2", Actions: []string{"NewAction"}},
			},
		},
	}

	roles := StaticDeriveRoles(policies)
	if len(roles) != 1 {
		t.Fatalf("expected 1 role, got %d", len(roles))
	}
	if roles[0].DisplayName != "V2" {
		t.Errorf("expected V2, got %q", roles[0].DisplayName)
	}
	if roles[0].BirthTimestamp != "003" {
		t.Errorf("expected birth timestamp 003, got %q", roles[0].BirthTimestamp)
	}
	// OldAction should NOT carry over
	if len(roles[0].Actions) != 1 || roles[0].Actions[0] != "NewAction" {
		t.Errorf("expected only NewAction, got %v", roles[0].Actions)
	}
}

func TestStaticDeriveRoles_ManagesIncludedInGate(t *testing.T) {
	policies := []StaticPolicyOps{
		{
			PolicyID: "001",
			Ops: []StaticRoleOp{
				{Type: "create", Slug: "admin", IsManages: true},
				{Type: "create", Slug: "moderator", Actions: []string{"BanUser"}},
				{Type: "create", Slug: "viewer"},
			},
		},
	}

	roles := StaticDeriveRoles(policies)
	grants := DeriveActionGrants(roles)

	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}

	grant := grants[0]
	if grant.ActionName != "BanUser" {
		t.Errorf("expected BanUser, got %q", grant.ActionName)
	}
	if len(grant.AllowedRoles) != 1 || grant.AllowedRoles[0] != "moderator" {
		t.Errorf("expected [moderator], got %v", grant.AllowedRoles)
	}
	if len(grant.ManagesRoles) != 1 || grant.ManagesRoles[0] != "admin" {
		t.Errorf("expected [admin] as manages, got %v", grant.ManagesRoles)
	}
}

func TestStaticDeriveRoles_RevokeCanRemovesFromGate(t *testing.T) {
	policies := []StaticPolicyOps{
		{
			PolicyID: "001",
			Ops: []StaticRoleOp{
				{Type: "create", Slug: "moderator", Actions: []string{"BanUser", "MuteUser"}},
			},
		},
		{
			PolicyID: "002",
			Ops: []StaticRoleOp{
				{Type: "alter", Slug: "moderator", RevokeActions: []string{"BanUser"}},
			},
		},
	}

	roles := StaticDeriveRoles(policies)
	grants := DeriveActionGrants(roles)

	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].ActionName != "MuteUser" {
		t.Errorf("expected MuteUser only, got %q", grants[0].ActionName)
	}
}

func TestStaticDeriveRoles_RoleWithoutCanExcluded(t *testing.T) {
	policies := []StaticPolicyOps{
		{
			PolicyID: "001",
			Ops: []StaticRoleOp{
				{Type: "create", Slug: "moderator", Actions: []string{"BanUser"}},
				{Type: "create", Slug: "viewer"}, // no Can()
			},
		},
	}

	roles := StaticDeriveRoles(policies)
	grants := DeriveActionGrants(roles)

	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	for _, role := range grants[0].AllowedRoles {
		if role == "viewer" {
			t.Error("viewer should not be in allowed roles")
		}
	}
}

func TestGenerateRBACGates_OverridePattern(t *testing.T) {
	projectDir := t.TempDir()
	policiesDir := filepath.Join(projectDir, "database", "policies")
	actionsDir := filepath.Join(projectDir, "database", "actions")
	userModelDir := filepath.Join(actionsDir, "user")

	os.MkdirAll(policiesDir, 0755)
	os.MkdirAll(userModelDir, 0755)

	// Policy with Can("Ban")
	writeTestFile(t, policiesDir, "2026_03_01_100000_create_roles.go", `package policies

type CreateRoles_2026_03_01_100000 struct{ Policy }

func (m *CreateRoles_2026_03_01_100000) Up() {
	m.CreateRole("moderator").Name("Moderator").Can("Ban", "Mute")
}

func (m *CreateRoles_2026_03_01_100000) Down() {}
`)

	// Action file for Ban
	writeTestFile(t, userModelDir, "ban.go", `package user

type BanAction struct{}

func (a *BanAction) Ban(ctx interface{}, model interface{}) error { return nil }
`)

	// Action file for Mute
	writeTestFile(t, userModelDir, "mute.go", `package user

type MuteAction struct{}

func (a *MuteAction) Mute(ctx interface{}, model interface{}) error { return nil }
`)

	// User-written gate for Ban (should suppress generated gate)
	writeTestFile(t, userModelDir, "ban_gate.go", `package user

func CanBan(ctx interface{}, model interface{}) bool { return true }
`)

	gates, err := GenerateRBACGates(actionsDir, policiesDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ban should NOT have a generated gate (user override)
	for path := range gates {
		if strings.Contains(path, "ban_gate_gen.go") {
			t.Errorf("ban_gate_gen.go should be suppressed by user-written ban_gate.go")
		}
	}

	// Mute should have a generated gate
	foundMute := false
	for path, src := range gates {
		if strings.Contains(path, "mute_gate_gen.go") {
			foundMute = true
			if !strings.Contains(string(src), "CanMute") {
				t.Error("expected CanMute in generated gate")
			}
			if !strings.Contains(string(src), `"moderator"`) {
				t.Error("expected moderator in generated gate")
			}
		}
	}
	if !foundMute {
		t.Error("expected mute_gate_gen.go to be generated")
	}
}

func TestGenerateRBACGates_DefaultAssignRoleGate(t *testing.T) {
	projectDir := t.TempDir()
	policiesDir := filepath.Join(projectDir, "database", "policies")
	actionsDir := filepath.Join(projectDir, "database", "actions")

	os.MkdirAll(policiesDir, 0755)
	os.MkdirAll(actionsDir, 0755)

	writeTestFile(t, policiesDir, "2026_03_01_100000_create_roles.go", `package policies

type CreateRoles_2026_03_01_100000 struct{ Policy }

func (m *CreateRoles_2026_03_01_100000) Up() {
	m.CreateRole("admin").Name("Administrator").Manages()
}

func (m *CreateRoles_2026_03_01_100000) Down() {}
`)

	gates, err := GenerateRBACGates(actionsDir, policiesDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundAssign := false
	for path, src := range gates {
		if strings.Contains(path, "assign_gate_gen.go") {
			foundAssign = true
			content := string(src)
			if !strings.Contains(content, "CanAssign") {
				t.Error("expected CanAssign function")
			}
			if !strings.Contains(content, "IsAdmin()") {
				t.Error("expected IsAdmin() check")
			}
		}
	}
	if !foundAssign {
		t.Error("expected assign_gate_gen.go to be generated")
	}
}

func TestGenerateRBACGates_AssignRoleOverride(t *testing.T) {
	projectDir := t.TempDir()
	policiesDir := filepath.Join(projectDir, "database", "policies")
	actionsDir := filepath.Join(projectDir, "database", "actions")
	roleDir := filepath.Join(actionsDir, "role")

	os.MkdirAll(policiesDir, 0755)
	os.MkdirAll(roleDir, 0755)

	writeTestFile(t, policiesDir, "2026_03_01_100000_create_roles.go", `package policies

type CreateRoles_2026_03_01_100000 struct{ Policy }

func (m *CreateRoles_2026_03_01_100000) Up() {
	m.CreateRole("admin").Name("Administrator").Manages()
}

func (m *CreateRoles_2026_03_01_100000) Down() {}
`)

	// User-written assign gate
	writeTestFile(t, roleDir, "assign_gate.go", `package role

func CanAssign(ctx interface{}, model interface{}) bool { return true }
`)

	gates, err := GenerateRBACGates(actionsDir, policiesDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for path := range gates {
		if strings.Contains(path, "assign_gate_gen.go") {
			t.Error("assign_gate_gen.go should be suppressed by user-written assign_gate.go")
		}
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}
