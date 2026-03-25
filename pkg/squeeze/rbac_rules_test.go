package squeeze

import (
	"testing"
)

func TestStaleRoleAnnotation_FlagsRemovedRole(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	EditorSees()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{"Editor": true},
		},
	}
	findings := ruleStaleRoleAnnotation(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "stale_role_annotation" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", findings[0].Severity)
	}
}

func TestStaleRoleAnnotation_IgnoresActiveRole(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	AdminSees()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{},
		},
	}
	findings := ruleStaleRoleAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestStaleRoleAnnotation_NilRoles(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	EditorSees()
}
`)
	ctx := &AnalysisContext{
		Methods:   map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: nil,
	}
	findings := ruleStaleRoleAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when RBACRoles is nil, got %d", len(findings))
	}
}

func TestUnknownRoleAnnotation_FlagsUndefinedRole(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	SupervisorSees()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true, "User": true},
			Removed: map[string]bool{"Editor": true},
		},
	}
	findings := ruleUnknownRoleAnnotation(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "unknown_role_annotation" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestUnknownRoleAnnotation_IgnoresDefinedRole(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	AdminSees()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{},
		},
	}
	findings := ruleUnknownRoleAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestUnknownRoleAnnotation_IgnoresRemovedRole(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	EditorSees()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{"Editor": true},
		},
	}
	findings := ruleUnknownRoleAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestUnknownRoleAnnotation_NilRoles(t *testing.T) {
	ctx := &AnalysisContext{
		Methods:   map[string]*ControllerMethod{},
		RBACRoles: nil,
	}
	findings := ruleUnknownRoleAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when RBACRoles is nil, got %d", len(findings))
	}
}

func TestRoleWithoutLoad_FlagsMissingLoadRoles(t *testing.T) {
	ctx := &AnalysisContext{
		Routes: []AnalyzedRoute{
			{
				Method:     "GET",
				Path:       "/admin/users",
				Middleware: []string{"Auth", "RequireRole"},
				File:       "routes/web.go",
				Line:       10,
			},
		},
	}
	findings := ruleRoleWithoutLoad(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "role_without_load" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestRoleWithoutLoad_PassesWithLoadRoles(t *testing.T) {
	ctx := &AnalysisContext{
		Routes: []AnalyzedRoute{
			{
				Method:     "GET",
				Path:       "/admin/users",
				Middleware: []string{"Auth", "LoadRoles", "RequireRole"},
				File:       "routes/web.go",
				Line:       10,
			},
		},
	}
	findings := ruleRoleWithoutLoad(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestRoleWithoutLoad_IgnoresRoutesWithoutRequireRole(t *testing.T) {
	ctx := &AnalysisContext{
		Routes: []AnalyzedRoute{
			{
				Method:     "GET",
				Path:       "/public",
				Middleware: []string{"RateLimit"},
				File:       "routes/web.go",
				Line:       5,
			},
		},
	}
	findings := ruleRoleWithoutLoad(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestDefaultRoleMissing_NoDefaults(t *testing.T) {
	ctx := &AnalysisContext{
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true, "User": true},
			Removed: map[string]bool{},
		},
		RBACDefaults: nil,
	}
	findings := ruleDefaultRoleMissing(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "default_role_missing" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestDefaultRoleMissing_MultipleDefaults(t *testing.T) {
	ctx := &AnalysisContext{
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true, "User": true},
			Removed: map[string]bool{},
		},
		RBACDefaults: []RBACDefault{
			{Role: "Admin", File: "config/roles.go", Line: 5},
			{Role: "User", File: "config/roles.go", Line: 10},
		},
	}
	findings := ruleDefaultRoleMissing(ctx)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	for _, f := range findings {
		if f.Rule != "default_role_missing" {
			t.Errorf("rule = %q", f.Rule)
		}
	}
}

func TestDefaultRoleMissing_ExactlyOneDefault(t *testing.T) {
	ctx := &AnalysisContext{
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true, "User": true},
			Removed: map[string]bool{},
		},
		RBACDefaults: []RBACDefault{
			{Role: "User", File: "config/roles.go", Line: 10},
		},
	}
	findings := ruleDefaultRoleMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestDefaultRoleMissing_NilRoles(t *testing.T) {
	ctx := &AnalysisContext{
		RBACRoles: nil,
	}
	findings := ruleDefaultRoleMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no roles, got %d", len(findings))
	}
}

func TestDefaultRoleMissing_EmptyDefined(t *testing.T) {
	ctx := &AnalysisContext{
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{},
			Removed: map[string]bool{},
		},
	}
	findings := ruleDefaultRoleMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no roles defined, got %d", len(findings))
	}
}

func TestStaleRoleAnnotation_SelectorCall(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	t.EditorSees()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{"Editor": true},
		},
	}
	findings := ruleStaleRoleAnnotation(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for selector call, got %d", len(findings))
	}
}
