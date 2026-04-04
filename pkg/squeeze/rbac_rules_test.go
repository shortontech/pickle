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

// ---- pre_birth_annotation ----

func TestPreBirthAnnotation_FlagsPreBirth(t *testing.T) {
	m := method(t, `package migrations
func Up() {
	ModeratorSees()
}
`)
	m.File = "database/migrations/2026_04_10_create_posts.go"
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"Migration.Up": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Moderator": true},
			Removed: map[string]bool{},
		},
		RoleBirths: map[string]string{"Moderator": "2026_11_15_000001"},
	}
	findings := rulePreBirthAnnotation(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "pre_birth_annotation" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", findings[0].Severity)
	}
}

func TestPreBirthAnnotation_IgnoresPostBirth(t *testing.T) {
	m := method(t, `package migrations
func Up() {
	ModeratorSees()
}
`)
	m.File = "database/migrations/2026_12_01_create_posts.go"
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"Migration.Up": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Moderator": true},
			Removed: map[string]bool{},
		},
		RoleBirths: map[string]string{"Moderator": "2026_11_15_000001"},
	}
	findings := rulePreBirthAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestPreBirthAnnotation_NilRoles(t *testing.T) {
	ctx := &AnalysisContext{
		Methods:   map[string]*ControllerMethod{},
		RBACRoles: nil,
	}
	findings := rulePreBirthAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when nil roles, got %d", len(findings))
	}
}

func TestPreBirthAnnotation_NoBirths(t *testing.T) {
	m := method(t, `package migrations
func Up() {
	ModeratorSees()
}
`)
	m.File = "database/migrations/2026_04_10_create_posts.go"
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"Migration.Up": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Moderator": true},
			Removed: map[string]bool{},
		},
		RoleBirths: map[string]string{},
	}
	findings := rulePreBirthAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no births, got %d", len(findings))
	}
}

func TestPreBirthAnnotation_NonMigrationFile(t *testing.T) {
	m := method(t, `package controllers
func Up() {
	ModeratorSees()
}
`)
	m.File = "app/http/controllers/post_controller.go"
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Up": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Moderator": true},
			Removed: map[string]bool{},
		},
		RoleBirths: map[string]string{"Moderator": "2026_01_01_000001"},
	}
	findings := rulePreBirthAnnotation(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for non-migration file, got %d", len(findings))
	}
}

// ---- missing_visibility_scope ----

func TestMissingVisibilityScope_Flags(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	QueryPost()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/api/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth", "LoadRoles"}, File: "routes/web.go", Line: 10},
		},
		RBACRoles:            &RBACRoleSet{Defined: map[string]bool{"Admin": true}, Removed: map[string]bool{}},
		TablesWithVisibility: map[string]bool{"Post": true},
	}
	findings := ruleMissingVisibilityScope(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "missing_visibility_scope" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestMissingVisibilityScope_PassesWithSelectForRoles(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	QueryPost().SelectForRoles(roles)
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/api/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth", "LoadRoles"}, File: "routes/web.go", Line: 10},
		},
		RBACRoles:            &RBACRoleSet{Defined: map[string]bool{"Admin": true}, Removed: map[string]bool{}},
		TablesWithVisibility: map[string]bool{"Post": true},
	}
	findings := ruleMissingVisibilityScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestMissingVisibilityScope_PassesWithSelectAll(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	QueryPost().SelectAll()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/api/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth", "LoadRoles"}, File: "routes/web.go", Line: 10},
		},
		RBACRoles:            &RBACRoleSet{Defined: map[string]bool{"Admin": true}, Removed: map[string]bool{}},
		TablesWithVisibility: map[string]bool{"Post": true},
	}
	findings := ruleMissingVisibilityScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestMissingVisibilityScope_IgnoresWithoutLoadRoles(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	QueryPost()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/api/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth"}, File: "routes/web.go", Line: 10},
		},
		RBACRoles:            &RBACRoleSet{Defined: map[string]bool{"Admin": true}, Removed: map[string]bool{}},
		TablesWithVisibility: map[string]bool{"Post": true},
	}
	findings := ruleMissingVisibilityScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings without LoadRoles, got %d", len(findings))
	}
}

func TestMissingVisibilityScope_NilRoles(t *testing.T) {
	ctx := &AnalysisContext{
		Methods:   map[string]*ControllerMethod{},
		RBACRoles: nil,
	}
	findings := ruleMissingVisibilityScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when nil roles, got %d", len(findings))
	}
}

func TestMissingVisibilityScope_NoVisibleTables(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	QueryPost()
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/api/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth", "LoadRoles"}, File: "routes/web.go", Line: 10},
		},
		RBACRoles:            &RBACRoleSet{Defined: map[string]bool{"Admin": true}, Removed: map[string]bool{}},
		TablesWithVisibility: map[string]bool{},
	}
	findings := ruleMissingVisibilityScope(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no visible tables, got %d", len(findings))
	}
}

// ---- hardcoded_role_select ----

func TestHardcodedRoleSelect_FlagsLiteral(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q.SelectFor("administrator")
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{},
		},
	}
	findings := ruleHardcodedRoleSelect(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "hardcoded_role_select" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", findings[0].Severity)
	}
}

func TestHardcodedRoleSelect_AllowsVariable(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q.SelectFor(ctx.Role())
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"PostController.Index": m},
		RBACRoles: &RBACRoleSet{
			Defined: map[string]bool{"Admin": true},
			Removed: map[string]bool{},
		},
	}
	findings := ruleHardcodedRoleSelect(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestHardcodedRoleSelect_NilRoles(t *testing.T) {
	ctx := &AnalysisContext{
		Methods:   map[string]*ControllerMethod{},
		RBACRoles: nil,
	}
	findings := ruleHardcodedRoleSelect(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when nil roles, got %d", len(findings))
	}
}

