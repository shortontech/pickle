package generator

import (
	"strings"
	"testing"
)

func TestGenerateActionWiringWithAudit_Basic(t *testing.T) {
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

	src, err := GenerateActionWiringWithAudit(set, "models", "myapp/database/actions/user", "myapp/app/http", "myapp/app/models/audit")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "func (m *User) Ban(ctx *pickle.Context, action actions.BanAction) error") {
		t.Error("expected Ban method on User")
	}
	if !strings.Contains(content, "actions.CanBan(ctx, m)") {
		t.Error("expected gate call")
	}
	if !strings.Contains(content, `pickle.AuditDenied(ctx, "Ban", "User", m.ID, "gate_check_failed")`) {
		t.Error("expected AuditDenied call on gate failure")
	}
	if !strings.Contains(content, `pickle.AuditFailed(ctx, "Ban", "User", m.ID, err)`) {
		t.Error("expected AuditFailed call on action error")
	}
	if !strings.Contains(content, "ErrUnauthorized") {
		t.Error("expected ErrUnauthorized check")
	}
	// Transactional wiring
	if !strings.Contains(content, "pickle.WithTransaction(func(tx *pickle.Tx) error") {
		t.Error("expected transactional wiring")
	}
	if !strings.Contains(content, "audit.Performed(ctx, audit.ActionTypeUserBan, m.ID, nil, roleID, tx.Conn())") {
		t.Error("expected audit.Performed call with nil version (mutable table)")
	}
	if !strings.Contains(content, "action.ban(ctx, m, tx.Conn())") {
		t.Error("expected action execute with tx")
	}
}

func TestGenerateActionWiringWithAudit_WithResult(t *testing.T) {
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

	src, err := GenerateActionWiringWithAudit(set, "models", "myapp/database/actions/customer", "myapp/app/http", "myapp/app/models/audit")
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
	if !strings.Contains(content, `pickle.AuditDenied(ctx, "InitiateTransfer", "Customer", m.ID, "gate_check_failed")`) {
		t.Error("expected AuditDenied for denied result action")
	}
	if !strings.Contains(content, "pickle.WithTransaction(func(tx *pickle.Tx) error") {
		t.Error("expected transactional wiring")
	}
	if !strings.Contains(content, "result, execErr = action.initiateTransfer(ctx, m, tx.Conn())") {
		t.Error("expected result capture from action call with tx")
	}
	if !strings.Contains(content, "return result, nil") {
		t.Error("expected result return on success")
	}
}

func TestGenerateActionWiringWithAudit_Immutable(t *testing.T) {
	set := &ActionSet{
		Model:       "transfer",
		IsImmutable: true,
		Actions: []ActionDef{
			{
				Name:       "Approve",
				StructName: "ApproveAction",
				SourceFile: "database/actions/transfer/approve.go",
			},
		},
		Gates: []GateDef{
			{ActionName: "Approve", Name: "CanApprove", SourceFile: "database/actions/transfer/approve_gate.go"},
		},
	}

	src, err := GenerateActionWiringWithAudit(set, "models", "myapp/database/actions/transfer", "myapp/app/http", "myapp/app/models/audit")
	if err != nil {
		t.Fatal(err)
	}
	content := string(src)

	if !strings.Contains(content, "&m.VersionID") {
		t.Error("expected &m.VersionID for immutable table")
	}
}

func TestGenerateActionWiringWithAudit_Empty(t *testing.T) {
	set := &ActionSet{Model: "user"}
	src, err := GenerateActionWiringWithAudit(set, "models", "myapp/database/actions/user", "myapp/app/http", "myapp/app/models/audit")
	if err != nil {
		t.Fatal(err)
	}
	if src != nil {
		t.Error("expected nil output for empty action set")
	}
}
