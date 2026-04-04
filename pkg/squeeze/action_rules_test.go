package squeeze

import (
	"testing"
)

func TestUngatedAction_FlagsMissingGate(t *testing.T) {
	ctx := &AnalysisContext{
		Actions: []ActionInfo{
			{
				Name:     "CreateTransfer",
				File:     "app/actions/create_transfer.go",
				GateFile: "app/actions/create_transfer_gate.go",
				HasGate:  false,
			},
		},
	}
	findings := ruleUngatedAction(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "ungated_action" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestUngatedAction_PassesWithGate(t *testing.T) {
	ctx := &AnalysisContext{
		Actions: []ActionInfo{
			{
				Name:     "CreateTransfer",
				File:     "app/actions/create_transfer.go",
				GateFile: "app/actions/create_transfer_gate.go",
				HasGate:  true,
			},
		},
	}
	findings := ruleUngatedAction(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestUngatedAction_MultipleActions(t *testing.T) {
	ctx := &AnalysisContext{
		Actions: []ActionInfo{
			{Name: "CreateTransfer", File: "app/actions/create_transfer.go", GateFile: "app/actions/create_transfer_gate.go", HasGate: true},
			{Name: "DeleteUser", File: "app/actions/delete_user.go", GateFile: "app/actions/delete_user_gate.go", HasGate: false},
			{Name: "UpdatePost", File: "app/actions/update_post.go", GateFile: "app/actions/update_post_gate.go", HasGate: false},
		},
	}
	findings := ruleUngatedAction(ctx)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
}

func TestUngatedAction_EmptyActions(t *testing.T) {
	ctx := &AnalysisContext{
		Actions: nil,
	}
	findings := ruleUngatedAction(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestDirectExecuteCall_FlagsExecuteInAction(t *testing.T) {
	m := method(t, `package actions
func Run() {
	action.ban()
}
`)
	m.File = "app/actions/create_transfer.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"CreateTransfer.Run": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleDirectExecuteCall(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "direct_execute_call" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestDirectExecuteCall_IgnoresNonActionFile(t *testing.T) {
	m := method(t, `package controllers
func Run() {
	action.ban()
}
`)
	m.File = "app/http/controllers/transfer_controller.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"TransferController.Run": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleDirectExecuteCall(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestDirectExecuteCall_IgnoresNonExecuteMethod(t *testing.T) {
	m := method(t, `package actions
func Run() {
	action.Validate()
}
`)
	m.File = "app/actions/create_transfer.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"CreateTransfer.Run": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleDirectExecuteCall(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestScopeBuilderLeak_FlagsOutsideScopes(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	sb := ScopeBuilder{}
}
`)
	m.File = "app/http/controllers/user_controller.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"UserController.Index": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleScopeBuilderLeak(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "scope_builder_leak" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestScopeBuilderLeak_AllowedInsideScopes(t *testing.T) {
	m := method(t, `package scopes
func Apply() {
	sb := ScopeBuilder{}
}
`)
	m.File = "database/scopes/user_scope.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"UserScope.Apply": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleScopeBuilderLeak(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestQueryBuilderInScope_FlagsQueryInScopes(t *testing.T) {
	m := method(t, `package scopes
func Apply() {
	models.QueryUser()
}
`)
	m.File = "database/scopes/user_scope.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"UserScope.Apply": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleQueryBuilderInScope(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "query_builder_in_scope" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestQueryBuilderInScope_AllowedOutsideScopes(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	models.QueryUser()
}
`)
	m.File = "app/http/controllers/user_controller.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"UserController.Index": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleQueryBuilderInScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestQueryBuilderInScope_IgnoresPlainQuery(t *testing.T) {
	m := method(t, `package scopes
func Apply() {
	Query()
}
`)
	m.File = "database/scopes/user_scope.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"UserScope.Apply": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleQueryBuilderInScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for bare Query(), got %d", len(findings))
	}
}

func TestQueryBuilderInScope_FlagsQueryPost(t *testing.T) {
	m := method(t, `package scopes
func Apply() {
	QueryPost()
}
`)
	m.File = "database/scopes/post_scope.go"
	ctx := &AnalysisContext{
		Methods:      map[string]*ControllerMethod{"PostScope.Apply": m},
		FuncRegistry: make(FuncRegistry),
	}
	findings := ruleQueryBuilderInScope(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

// ---- scope_side_effect ----

func TestScopeSideEffect_FlagsDisallowedCall(t *testing.T) {
	m := method(t, `package scopes
func Apply() {
	sb.First()
}
`)
	m.File = "database/scopes/user/active.go"
	ctx := &AnalysisContext{
		Methods:             map[string]*ControllerMethod{"UserScope.Apply": m},
		FuncRegistry:        make(FuncRegistry),
		ScopeAllowedMethods: map[string]bool{"Where": true, "WhereIn": true, "OrderBy": true},
	}
	findings := ruleScopeSideEffect(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "scope_side_effect" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestScopeSideEffect_AllowsAllowedCall(t *testing.T) {
	m := method(t, `package scopes
func Apply() {
	sb.Where(x)
}
`)
	m.File = "database/scopes/user/active.go"
	ctx := &AnalysisContext{
		Methods:             map[string]*ControllerMethod{"UserScope.Apply": m},
		FuncRegistry:        make(FuncRegistry),
		ScopeAllowedMethods: map[string]bool{"Where": true, "WhereIn": true, "OrderBy": true},
	}
	findings := ruleScopeSideEffect(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestScopeSideEffect_IgnoresNonScopeFile(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	sb.First()
}
`)
	m.File = "app/http/controllers/user_controller.go"
	ctx := &AnalysisContext{
		Methods:             map[string]*ControllerMethod{"UserController.Index": m},
		FuncRegistry:        make(FuncRegistry),
		ScopeAllowedMethods: map[string]bool{"Where": true},
	}
	findings := ruleScopeSideEffect(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for non-scope file, got %d", len(findings))
	}
}

func TestScopeSideEffect_NoAllowedMethods(t *testing.T) {
	ctx := &AnalysisContext{
		Methods:             map[string]*ControllerMethod{},
		FuncRegistry:        make(FuncRegistry),
		ScopeAllowedMethods: map[string]bool{},
	}
	findings := ruleScopeSideEffect(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no allowed methods, got %d", len(findings))
	}
}
