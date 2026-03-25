package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanActions(t *testing.T) {
	dir := t.TempDir()

	// Create database/actions/user/ban.go
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "ban.go"), []byte(`package user

type BanAction struct{}

func (a BanAction) Ban(ctx interface{}, model interface{}) error {
	return nil
}
`), 0o644)

	// Create ban_gate.go
	os.WriteFile(filepath.Join(userDir, "ban_gate.go"), []byte(`package user

func CanBan(ctx interface{}, model interface{}) *string {
	s := "role-id"
	return &s
}
`), 0o644)

	result, err := ScanActions(dir)
	if err != nil {
		t.Fatal(err)
	}

	set, ok := result["user"]
	if !ok {
		t.Fatal("expected actions for 'user'")
	}

	if len(set.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(set.Actions))
	}
	if set.Actions[0].Name != "Ban" {
		t.Errorf("expected action 'Ban', got %q", set.Actions[0].Name)
	}
	if set.Actions[0].StructName != "BanAction" {
		t.Errorf("expected struct 'BanAction', got %q", set.Actions[0].StructName)
	}

	if len(set.Gates) != 1 {
		t.Fatalf("expected 1 gate, got %d", len(set.Gates))
	}
	if set.Gates[0].Name != "CanBan" {
		t.Errorf("expected gate 'CanBan', got %q", set.Gates[0].Name)
	}
}

func TestValidateActionsPass(t *testing.T) {
	set := &ActionSet{
		Model:   "user",
		Actions: []ActionDef{{Name: "Ban"}},
		Gates:   []GateDef{{ActionName: "Ban", Name: "CanBan"}},
	}
	if err := ValidateActions(set); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateActionsMissingGate(t *testing.T) {
	set := &ActionSet{
		Model:   "user",
		Actions: []ActionDef{{Name: "Ban"}},
		Gates:   nil,
	}
	err := ValidateActions(set)
	if err == nil {
		t.Fatal("expected error for missing gate")
	}
	if !strings.Contains(err.Error(), "Ban") {
		t.Errorf("error should mention action name: %v", err)
	}
}

func TestGenerateActionWiring(t *testing.T) {
	set := &ActionSet{
		Model: "user",
		Actions: []ActionDef{
			{
				Name:       "Ban",
				StructName: "BanAction",
				SourceFile: "database/actions/user/ban.go",
			},
		},
		Gates: []GateDef{
			{
				ActionName: "Ban",
				Name:       "CanBan",
				SourceFile: "database/actions/user/ban_gate.go",
			},
		},
	}

	src, err := GenerateActionWiring(set, "models", "myapp/database/actions/user")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "func (m *User) Ban(ctx *Context, action actions.BanAction) error") {
		t.Error("expected Ban method on User")
	}
	if !strings.Contains(content, "actions.CanBan(ctx, m)") {
		t.Error("expected gate call")
	}
	if !strings.Contains(content, "action.ban(ctx, m)") {
		t.Error("expected lowercased action method call")
	}
	if !strings.Contains(content, "ErrUnauthorized") {
		t.Error("expected ErrUnauthorized check")
	}
	if !strings.Contains(content, "// Ban — action source: database/actions/user/ban.go") {
		t.Error("expected source comment")
	}
}

func TestGenerateActionWiringWithResult(t *testing.T) {
	set := &ActionSet{
		Model: "customer",
		Actions: []ActionDef{
			{
				Name:       "InitiateTransfer",
				StructName: "InitiateTransferAction",
				SourceFile: "database/actions/customer/initiate_transfer.go",
				HasResult:  true,
				ResultType: "*TransferResult",
			},
		},
		Gates: []GateDef{
			{ActionName: "InitiateTransfer", Name: "CanInitiateTransfer", SourceFile: "database/actions/customer/initiate_transfer_gate.go"},
		},
	}

	src, err := GenerateActionWiring(set, "models", "myapp/database/actions/customer")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "(*TransferResult, error)") {
		t.Error("expected result type in signature")
	}
	if !strings.Contains(content, "return nil, ErrUnauthorized") {
		t.Error("expected nil, ErrUnauthorized for result actions")
	}
}

func TestScanActionsEmpty(t *testing.T) {
	result, err := ScanActions("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Error("expected empty result")
	}
}

func TestStandaloneGate(t *testing.T) {
	set := &ActionSet{
		Model: "user",
		Gates: []GateDef{{ActionName: "ViewProfile", Name: "CanViewProfile"}},
	}
	// Should validate with no error (no actions to require gates for)
	if err := ValidateActions(set); err != nil {
		t.Errorf("standalone gates should not cause validation error: %v", err)
	}
}
