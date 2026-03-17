package squeeze

import (
	"go/ast"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

// ---- helpers ----

func defaultConfig() SqueezeConfig {
	return SqueezeConfig{
		Middleware: MiddlewareConfig{
			Auth:      []string{"Auth"},
			Admin:     []string{"RequireAdmin"},
			RateLimit: []string{"RateLimit"},
			CSRF:      []string{"CSRF"},
		},
	}
}

func method(t *testing.T, src string) *ControllerMethod {
	t.Helper()
	body, fset, _ := parseFunc(t, src)
	return &ControllerMethod{Body: body, Fset: fset, File: "controllers/test.go", Line: 1}
}

// ---- Finding / Severity ----

func TestSeverityString(t *testing.T) {
	if SeverityWarning.String() != "warning" {
		t.Errorf("got %q", SeverityWarning.String())
	}
	if SeverityError.String() != "error" {
		t.Errorf("got %q", SeverityError.String())
	}
	if Severity(99).String() != "unknown" {
		t.Errorf("got %q", Severity(99).String())
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Rule: "no_printf", Severity: SeverityWarning, File: "ctrl.go", Line: 42, Message: "bad"}
	s := f.String()
	if s != "ctrl.go:42: [warning] no_printf: bad" {
		t.Errorf("unexpected: %s", s)
	}
}

// ---- Config ----

func TestRuleEnabled_DefaultsTrue(t *testing.T) {
	cfg := SqueezeConfig{}
	if !cfg.RuleEnabled("no_printf") {
		t.Error("rule should default to enabled when Rules is nil")
	}
}

func TestRuleEnabled_ExplicitDisable(t *testing.T) {
	cfg := SqueezeConfig{Rules: map[string]bool{"no_printf": false}}
	if cfg.RuleEnabled("no_printf") {
		t.Error("rule should be disabled")
	}
}

func TestRuleEnabled_ExplicitEnable(t *testing.T) {
	cfg := SqueezeConfig{Rules: map[string]bool{"no_printf": true}}
	if !cfg.RuleEnabled("no_printf") {
		t.Error("rule should be enabled")
	}
}

func TestRuleEnabled_UnknownRuleDefaultsTrue(t *testing.T) {
	cfg := SqueezeConfig{Rules: map[string]bool{"other": false}}
	if !cfg.RuleEnabled("no_printf") {
		t.Error("unknown rule should default to enabled")
	}
}

func TestMiddlewareConfig_IsAuthMiddleware(t *testing.T) {
	mc := MiddlewareConfig{Auth: []string{"Auth"}, Admin: []string{"RequireAdmin"}}
	if !mc.IsAuthMiddleware("Auth") {
		t.Error("Auth should be auth middleware")
	}
	if !mc.IsAuthMiddleware("RequireAdmin") {
		t.Error("Admin implies auth")
	}
	if mc.IsAuthMiddleware("RateLimit") {
		t.Error("RateLimit should not be auth")
	}
}

func TestMiddlewareConfig_IsAdminMiddleware(t *testing.T) {
	mc := MiddlewareConfig{Admin: []string{"RequireAdmin"}}
	if !mc.IsAdminMiddleware("RequireAdmin") {
		t.Error("RequireAdmin should be admin")
	}
	if mc.IsAdminMiddleware("Auth") {
		t.Error("Auth is not admin")
	}
}

func TestMiddlewareConfig_IsRateLimitMiddleware(t *testing.T) {
	mc := MiddlewareConfig{RateLimit: []string{"RateLimit"}}
	if !mc.IsRateLimitMiddleware("RateLimit") {
		t.Error("RateLimit should be rate limit")
	}
}

func TestMiddlewareConfig_IsCSRFMiddleware_DefaultName(t *testing.T) {
	mc := MiddlewareConfig{} // no CSRF configured → default to "CSRF"
	if !mc.IsCSRFMiddleware("CSRF") {
		t.Error("default CSRF name should match")
	}
	if mc.IsCSRFMiddleware("VerifyCSRF") {
		t.Error("non-default name should not match when unconfigured")
	}
}

func TestMiddlewareConfig_IsCSRFMiddleware_Custom(t *testing.T) {
	mc := MiddlewareConfig{CSRF: []string{"VerifyCSRF"}}
	if !mc.IsCSRFMiddleware("VerifyCSRF") {
		t.Error("custom CSRF name should match")
	}
	if mc.IsCSRFMiddleware("CSRF") {
		t.Error("default name should not match when custom is set")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := LoadConfig("/does/not/exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil default config")
	}
}

// ---- Route helpers ----

func TestRouteParams(t *testing.T) {
	params := RouteParams("/users/:id/posts/:postId")
	if len(params) != 2 || params[0] != "id" || params[1] != "postId" {
		t.Errorf("unexpected params: %v", params)
	}
}

func TestRouteParams_NoParams(t *testing.T) {
	params := RouteParams("/users")
	if len(params) != 0 {
		t.Errorf("expected no params, got %v", params)
	}
}

func TestAnalyzedRoute_HasAuthMiddleware(t *testing.T) {
	mc := MiddlewareConfig{Auth: []string{"Auth"}}
	r := AnalyzedRoute{Middleware: []string{"RateLimit", "Auth"}}
	if !r.HasAuthMiddleware(mc) {
		t.Error("should have auth middleware")
	}
	r2 := AnalyzedRoute{Middleware: []string{"RateLimit"}}
	if r2.HasAuthMiddleware(mc) {
		t.Error("should not have auth middleware")
	}
}

func TestAnalyzedRoute_HasAdminMiddleware(t *testing.T) {
	mc := MiddlewareConfig{Admin: []string{"RequireAdmin"}}
	r := AnalyzedRoute{Middleware: []string{"RequireAdmin"}}
	if !r.HasAdminMiddleware(mc) {
		t.Error("should have admin middleware")
	}
}

func TestAnalyzedRoute_HasRateLimitMiddleware(t *testing.T) {
	mc := MiddlewareConfig{RateLimit: []string{"RateLimit"}}
	r := AnalyzedRoute{Middleware: []string{"RateLimit"}}
	if !r.HasRateLimitMiddleware(mc) {
		t.Error("should have rate limit middleware")
	}
}

func TestAnalyzedRoute_HasCSRFMiddleware(t *testing.T) {
	mc := MiddlewareConfig{}
	r := AnalyzedRoute{Middleware: []string{"CSRF"}}
	if !r.HasCSRFMiddleware(mc) {
		t.Error("should have CSRF middleware")
	}
}

// ---- AST helper functions ----

func TestFindCallsTo_FindsFmtPrintf(t *testing.T) {
	src := `package controllers
import "fmt"
func Handler() {
	fmt.Printf("hello %s", name)
}`
	m := method(t, src)
	lines := FindCallsTo(m.Body, m.Fset, "fmt", "Printf")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
}

func TestFindCallsTo_NoMatch(t *testing.T) {
	src := `package controllers
func Handler() {
	x := 1
	_ = x
}`
	m := method(t, src)
	lines := FindCallsTo(m.Body, m.Fset, "fmt", "Printf")
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestFindBuiltinCalls_FindsRecover(t *testing.T) {
	src := `package controllers
func Handler() {
	defer func() {
		r := recover()
		_ = r
	}()
}`
	m := method(t, src)
	lines := FindBuiltinCalls(m.Body, m.Fset, "recover")
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
}

func TestFindBuiltinCalls_NoMatch(t *testing.T) {
	src := `package controllers
func Handler() {
	x := 1
	_ = x
}`
	m := method(t, src)
	lines := FindBuiltinCalls(m.Body, m.Fset, "recover")
	if len(lines) != 0 {
		t.Errorf("expected 0, got %d", len(lines))
	}
}

func TestFindMustParseCalls_CtxParam(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	id := uuid.MustParse(ctx.Param("id"))
	_ = id
}`
	m := method(t, src)
	calls := FindMustParseCalls(m.Body, m.Fset)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !calls[0].HasCtxParam {
		t.Error("expected HasCtxParam=true")
	}
	if calls[0].HasCtxAuth {
		t.Error("expected HasCtxAuth=false")
	}
}

func TestFindMustParseCalls_CtxAuth(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	id := uuid.MustParse(ctx.Auth().UserID)
	_ = id
}`
	m := method(t, src)
	calls := FindMustParseCalls(m.Body, m.Fset)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !calls[0].HasCtxAuth {
		t.Error("expected HasCtxAuth=true")
	}
}

func TestFindMustParseCalls_NoCtxUsage(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	id := uuid.MustParse("abc")
	_ = id
}`
	m := method(t, src)
	calls := FindMustParseCalls(m.Body, m.Fset)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].HasCtxParam || calls[0].HasCtxAuth {
		t.Error("expected no ctx flags")
	}
}

func TestFindParamNames(t *testing.T) {
	src := `package controllers
func Handler() {
	id := ctx.Param("id")
	_ = id
}`
	m := method(t, src)
	calls := FindParamNames(m.Body, m.Fset)
	if len(calls) != 1 || calls[0].Name != "id" {
		t.Errorf("expected param 'id', got %v", calls)
	}
}

func TestFindParamNames_ParamUUID(t *testing.T) {
	src := `package controllers
func Handler() {
	id := ctx.ParamUUID("userId")
	_ = id
}`
	m := method(t, src)
	calls := FindParamNames(m.Body, m.Fset)
	if len(calls) != 1 || calls[0].Name != "userId" {
		t.Errorf("expected param 'userId', got %v", calls)
	}
}

func TestFindAuthTaintedVars(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	authID, err := uuid.Parse(ctx.Auth().UserID)
	_ = err
	_ = authID
}`
	m := method(t, src)
	vars := FindAuthTaintedVars(m.Body)
	if !vars["authID"] {
		t.Error("expected authID to be tainted")
	}
	if vars["err"] {
		t.Error("err should not be tainted")
	}
}

func TestFindModelVars_QueryResult(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	user, err := models.QueryUser().WhereID(id).First()
	_, _ = user, err
}`
	m := method(t, src)
	vars := FindModelVars(m.Body)
	if !vars["user"] {
		t.Error("expected 'user' to be a model var")
	}
}

func TestFindModelVars_CompositeLit(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{Title: "x"}
	_ = post
}`
	m := method(t, src)
	vars := FindModelVars(m.Body)
	if !vars["post"] {
		t.Error("expected 'post' to be a model var")
	}
}

func TestFindModelVars_CompositeLitWithoutAmpersand(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := models.Post{Title: "x"}
	_ = post
}`
	m := method(t, src)
	vars := FindModelVars(m.Body)
	if !vars["post"] {
		t.Error("expected 'post' to be a model var without &")
	}
}

func TestPayloadIsModelWithoutPublic_BareVar(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	user, _ := models.QueryUser().First()
	_ = user
}`
	m := method(t, src)
	vars := FindModelVars(m.Body)
	jsonCalls := FindCtxJSONCalls(m.Body, m.Fset)
	// No JSON calls in this body — just testing the helper
	_ = jsonCalls
	// Manually build a test ident
	if !PayloadIsModelWithoutPublic(makeIdent("user"), vars) {
		t.Error("expected bare user var to be flagged")
	}
}

func TestPayloadIsModelWithoutPublic_PublicCall(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	user, _ := models.QueryUser().First()
	_ = user
}`
	m := method(t, src)
	vars := FindModelVars(m.Body)
	// .Public() call should NOT be flagged
	call := makePublicCall()
	if PayloadIsModelWithoutPublic(call, vars) {
		t.Error("Public() call should not be flagged")
	}
}

func TestPayloadIsModelWithoutPublic_UnknownIdent(t *testing.T) {
	// 'result' is not a model var, should not be flagged
	vars := map[string]bool{"user": true}
	if PayloadIsModelWithoutPublic(makeIdent("result"), vars) {
		t.Error("unknown ident should not be flagged")
	}
}

// makeIdent builds a minimal *ast.Ident for testing
func makeIdent(name string) *ast.Ident {
	return &ast.Ident{Name: name}
}

// makePublicCall builds a minimal user.Public() call expression
func makePublicCall() *ast.CallExpr {
	return &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   makeIdent("user"),
			Sel: makeIdent("Public"),
		},
	}
}

// makeIdentExpr wraps makeIdent as ast.Expr
func makeIdentExpr(name string) ast.Expr {
	return makeIdent(name)
}

// ---- Rule: no_printf ----

func TestRuleNoPrintf_FlagsPrintf(t *testing.T) {
	src := `package controllers
import "fmt"
func (c UserController) Index() {
	fmt.Printf("hello")
}`
	body, fset, _ := parseFunc(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"UserController.Index": {Body: body, Fset: fset, File: "ctrl.go", Line: 1},
		},
	}
	findings := ruleNoPrintf(ctx)
	if len(findings) == 0 {
		t.Error("expected finding for fmt.Printf")
	}
}

func TestRuleNoPrintf_FlagsMultipleFunctions(t *testing.T) {
	src := `package controllers
import "fmt"
func Handler() {
	fmt.Println("a")
	fmt.Sprintf("b %s", x)
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleNoPrintf(ctx)
	if len(findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(findings))
	}
}

func TestRuleNoPrintf_NoFindings(t *testing.T) {
	src := `package controllers
func Handler() {
	x := 1
	_ = x
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleNoPrintf(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

// ---- Rule: no_recover ----

func TestRuleNoRecover_FlagsInController(t *testing.T) {
	src := `package controllers
func Handler() {
	defer func() {
		r := recover()
		_ = r
	}()
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleNoRecover(ctx)
	if len(findings) == 0 {
		t.Error("expected finding for recover()")
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected error severity, got %s", findings[0].Severity)
	}
}

func TestRuleNoRecover_FlagsInFuncRegistry(t *testing.T) {
	src := `package services
func DoWork() {
	defer func() {
		r := recover()
		_ = r
	}()
}`
	body, fset, _ := parseFunc(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{},
		FuncRegistry: FuncRegistry{
			"services.DoWork": {Body: body, Fset: fset},
		},
	}
	findings := ruleNoRecover(ctx)
	if len(findings) == 0 {
		t.Error("expected finding for recover() in func registry")
	}
}

func TestRuleNoRecover_NoFindings(t *testing.T) {
	src := `package controllers
func Handler() {
	x := 1
	_ = x
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleNoRecover(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

// ---- Rule: enum_validation ----

func TestRuleEnumValidation_FlagsStatusWithoutOneof(t *testing.T) {
	ctx := &AnalysisContext{
		Requests: []generator.RequestDef{
			{
				Name: "CreateTransferRequest",
				File: "requests/create_transfer.go",
				Fields: []generator.RequestField{
					{Name: "Status", Validate: "required"},
				},
			},
		},
	}
	findings := ruleEnumValidation(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "enum_validation" {
		t.Errorf("wrong rule: %s", findings[0].Rule)
	}
}

func TestRuleEnumValidation_PassesWithOneof(t *testing.T) {
	ctx := &AnalysisContext{
		Requests: []generator.RequestDef{
			{
				Name: "CreateTransferRequest",
				Fields: []generator.RequestField{
					{Name: "Status", Validate: "required,oneof=pending active"},
				},
			},
		},
	}
	findings := ruleEnumValidation(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with oneof, got %d", len(findings))
	}
}

func TestRuleEnumValidation_FlagsMultipleEnumFields(t *testing.T) {
	ctx := &AnalysisContext{
		Requests: []generator.RequestDef{
			{
				Name: "CreateUserRequest",
				Fields: []generator.RequestField{
					{Name: "Role", Validate: "required"},
					{Name: "Type", Validate: "required"},
				},
			},
		},
	}
	findings := ruleEnumValidation(ctx)
	if len(findings) != 2 {
		t.Errorf("expected 2 findings, got %d", len(findings))
	}
}

func TestRuleEnumValidation_IgnoresNonEnumField(t *testing.T) {
	ctx := &AnalysisContext{
		Requests: []generator.RequestDef{
			{
				Name: "CreateUserRequest",
				Fields: []generator.RequestField{
					{Name: "Email", Validate: "required,email"},
				},
			},
		},
	}
	findings := ruleEnumValidation(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-enum field, got %d", len(findings))
	}
}

// ---- Rule: uuid_error_handling ----

func TestRuleUUIDErrorHandling_CtxParamIsError(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	id := uuid.MustParse(ctx.Param("id"))
	_ = id
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleUUIDErrorHandling(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityError {
		t.Errorf("expected error severity")
	}
}

func TestRuleUUIDErrorHandling_CtxAuthIsWarning(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	id := uuid.MustParse(ctx.Auth().UserID)
	_ = id
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleUUIDErrorHandling(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity for ctx.Auth()")
	}
}

func TestRuleUUIDErrorHandling_NoCtxIsClean(t *testing.T) {
	src := `package controllers
import "uuid"
func Handler() {
	id := uuid.MustParse("static-uuid")
	_ = id
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"C.Handler": m},
	}
	findings := ruleUUIDErrorHandling(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for static UUID, got %d", len(findings))
	}
}

// ---- Rule: sensitive_field_encryption ----

func TestRuleSensitiveFieldEncryption_FlagsUnencrypted(t *testing.T) {
	ctx := &AnalysisContext{
		Tables: []*schema.Table{
			{
				Name: "users",
				Columns: []*schema.Column{
					{Name: "email", IsEncrypted: false},
				},
			},
		},
	}
	findings := ruleSensitiveFieldEncryption(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "sensitive_field_encryption" {
		t.Errorf("wrong rule: %s", findings[0].Rule)
	}
}

func TestRuleSensitiveFieldEncryption_PassesEncrypted(t *testing.T) {
	ctx := &AnalysisContext{
		Tables: []*schema.Table{
			{
				Name: "users",
				Columns: []*schema.Column{
					{Name: "email", IsEncrypted: true},
				},
			},
		},
	}
	findings := ruleSensitiveFieldEncryption(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for encrypted column, got %d", len(findings))
	}
}

func TestRuleSensitiveFieldEncryption_SuffixPatterns(t *testing.T) {
	cols := []string{"auth_token", "api_key", "reset_hash", "db_password"}
	for _, col := range cols {
		if !isSensitiveColumn(col) {
			t.Errorf("expected %q to be sensitive", col)
		}
	}
}

func TestRuleSensitiveFieldEncryption_NonSensitive(t *testing.T) {
	ctx := &AnalysisContext{
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsEncrypted: false},
				},
			},
		},
	}
	findings := ruleSensitiveFieldEncryption(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-sensitive column, got %d", len(findings))
	}
}

// ---- Rule: public_sensitive_conflict ----

func TestRulePublicSensitiveConflict_FlagsPublicSensitive(t *testing.T) {
	ctx := &AnalysisContext{
		Tables: []*schema.Table{
			{
				Name: "users",
				Columns: []*schema.Column{
					{Name: "password", IsPublic: true, IsUnsafePublic: false},
				},
			},
		},
	}
	findings := rulePublicSensitiveConflict(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityError {
		t.Error("expected error severity")
	}
}

func TestRulePublicSensitiveConflict_PassesUnsafePublic(t *testing.T) {
	ctx := &AnalysisContext{
		Tables: []*schema.Table{
			{
				Name: "users",
				Columns: []*schema.Column{
					{Name: "email", IsPublic: true, IsUnsafePublic: true},
				},
			},
		},
	}
	findings := rulePublicSensitiveConflict(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with UnsafePublic, got %d", len(findings))
	}
}

func TestRulePublicSensitiveConflict_PassesNonPublicSensitive(t *testing.T) {
	ctx := &AnalysisContext{
		Tables: []*schema.Table{
			{
				Name: "users",
				Columns: []*schema.Column{
					{Name: "password", IsPublic: false},
				},
			},
		},
	}
	findings := rulePublicSensitiveConflict(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-public sensitive, got %d", len(findings))
	}
}

// ---- Rule: rate_limit_auth ----

func TestRuleRateLimitAuth_FlagsLoginWithoutRateLimit(t *testing.T) {
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Routes: []AnalyzedRoute{
			{
				Method:         "POST",
				Path:           "/auth/login",
				ControllerType: "AuthController",
				MethodName:     "Login",
				Middleware:     []string{},
				File:           "routes/web.go",
				Line:           10,
			},
		},
	}
	findings := ruleRateLimitAuth(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRuleRateLimitAuth_PassesWithRateLimit(t *testing.T) {
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Routes: []AnalyzedRoute{
			{
				Method:         "POST",
				Path:           "/auth/login",
				ControllerType: "AuthController",
				MethodName:     "Login",
				Middleware:     []string{"RateLimit"},
				File:           "routes/web.go",
				Line:           10,
			},
		},
	}
	findings := ruleRateLimitAuth(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with RateLimit, got %d", len(findings))
	}
}

func TestRuleRateLimitAuth_SkipsNonAuthRoute(t *testing.T) {
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Routes: []AnalyzedRoute{
			{
				Method:         "POST",
				Path:           "/posts",
				ControllerType: "PostController",
				MethodName:     "Store",
				Middleware:     []string{},
				File:           "routes/web.go",
				Line:           10,
			},
		},
	}
	findings := ruleRateLimitAuth(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for non-auth route, got %d", len(findings))
	}
}

func TestRuleRateLimitAuth_SkipsGetRequests(t *testing.T) {
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Routes: []AnalyzedRoute{
			{
				Method:     "GET",
				Path:       "/login",
				MethodName: "Login",
				Middleware: []string{},
				File:       "routes/web.go",
				Line:       10,
			},
		},
	}
	findings := ruleRateLimitAuth(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for GET route, got %d", len(findings))
	}
}

func TestIsAuthRoute_PathMatch(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/api/login", true},
		{"/register", true},
		{"/auth/signup", true},
		{"/posts", false},
		{"/users", false},
	}
	for _, tt := range tests {
		route := AnalyzedRoute{Path: tt.path, ControllerType: "SomeController", MethodName: "Index"}
		if isAuthRoute(route) != tt.expected {
			t.Errorf("isAuthRoute(%q) = %v, want %v", tt.path, !tt.expected, tt.expected)
		}
	}
}

// ---- Rule: auth_without_middleware ----

func TestRuleAuthWithoutMiddleware_FlagsCtxAuthWithoutMiddleware(t *testing.T) {
	src := `package controllers
func Handler() {
	userID := ctx.Auth().UserID
	_ = userID
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/posts", ControllerType: "PostController", MethodName: "Store", Middleware: []string{}},
		},
	}
	findings := ruleAuthWithoutMiddleware(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRuleAuthWithoutMiddleware_PassesWithAuthMiddleware(t *testing.T) {
	src := `package controllers
func Handler() {
	userID := ctx.Auth().UserID
	_ = userID
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/posts", ControllerType: "PostController", MethodName: "Store", Middleware: []string{"Auth"}},
		},
	}
	findings := ruleAuthWithoutMiddleware(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when auth middleware present, got %d", len(findings))
	}
}

func TestRuleAuthWithoutMiddleware_PassesNoCtxAuth(t *testing.T) {
	src := `package controllers
func Handler() {
	x := 1
	_ = x
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{}},
		},
	}
	findings := ruleAuthWithoutMiddleware(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings without ctx.Auth(), got %d", len(findings))
	}
}

// ---- Rule: param_mismatch ----

func TestRuleParamMismatch_FlagsWrongParamName(t *testing.T) {
	src := `package controllers
func Handler() {
	id := ctx.Param("userId")
	_ = id
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Show": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Show"},
		},
	}
	findings := ruleParamMismatch(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "param_mismatch" {
		t.Errorf("wrong rule: %s", findings[0].Rule)
	}
}

func TestRuleParamMismatch_PassesCorrectParamName(t *testing.T) {
	src := `package controllers
func Handler() {
	id := ctx.Param("id")
	_ = id
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Show": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Show"},
		},
	}
	findings := ruleParamMismatch(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestRuleParamMismatch_SkipsUnknownController(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Show"},
		},
	}
	findings := ruleParamMismatch(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for unknown controller, got %d", len(findings))
	}
}

// ---- Rule: ownership_scoping ----

func TestRuleOwnershipScoping_FlagsMissingScope(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	models.QueryPost().WhereID(id).Delete()
}`
	m := method(t, src)
	m.File = "controllers/post.go"
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Destroy": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "DELETE", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Destroy", Middleware: []string{"Auth"}},
		},
	}
	findings := ruleOwnershipScoping(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRuleOwnershipScoping_PassesWithOwnershipWhere(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	authID := ctx.Auth().UserID
	models.QueryPost().WhereUserID(authID).WhereID(id).Delete()
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Destroy": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "DELETE", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Destroy", Middleware: []string{"Auth"}},
		},
	}
	findings := ruleOwnershipScoping(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with ownership scoping, got %d", len(findings))
	}
}

func TestRuleOwnershipScoping_SkipsAdminRoutes(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	models.QueryPost().WhereID(id).Delete()
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Destroy": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "DELETE", Path: "/admin/posts/:id", ControllerType: "PostController", MethodName: "Destroy", Middleware: []string{"RequireAdmin"}},
		},
	}
	findings := ruleOwnershipScoping(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for admin routes, got %d", len(findings))
	}
}

func TestRuleOwnershipScoping_SkipsNonAuthRoutes(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	models.QueryPost().WhereID(id).Delete()
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Destroy": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "DELETE", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Destroy", Middleware: []string{}},
		},
	}
	findings := ruleOwnershipScoping(ctx)
	if len(findings) != 0 {
		// No auth middleware -> scoping rule skips it
		t.Errorf("expected 0 findings for non-auth DELETE route, got %d", len(findings))
	}
}

func TestRuleOwnershipScoping_PassesAnyOwner(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	models.QueryPost().WhereID(id).AnyOwner().Delete()
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Destroy": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "DELETE", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Destroy", Middleware: []string{"Auth"}},
		},
	}
	findings := ruleOwnershipScoping(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for AnyOwner() opt-out, got %d", len(findings))
	}
}

// ---- Rule: read_scoping ----

func TestRuleReadScoping_FlagsMissingScope(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	posts, _ := models.QueryPost().All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth"}},
		},
	}
	findings := ruleReadScoping(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRuleReadScoping_PassesWithOwnershipWhere(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	authID := ctx.Auth().UserID
	posts, _ := models.QueryPost().WhereUserID(authID).All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"Auth"}},
		},
	}
	findings := ruleReadScoping(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestRuleReadScoping_SkipsAdminRoutes(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	posts, _ := models.QueryPost().All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/admin/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{"RequireAdmin"}},
		},
	}
	findings := ruleReadScoping(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for admin route, got %d", len(findings))
	}
}

func TestRuleReadScoping_SkipsNonAuthRoutes(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	posts, _ := models.QueryPost().All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index", Middleware: []string{}},
		},
	}
	findings := ruleReadScoping(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for unauthenticated GET, got %d", len(findings))
	}
}

// ---- Rule: unbounded_query ----

func TestRuleUnboundedQuery_FlagsAllWithoutLimit(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	posts, _ := models.QueryPost().All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index"},
		},
	}
	findings := ruleUnboundedQuery(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "unbounded_query" {
		t.Errorf("wrong rule: %s", findings[0].Rule)
	}
}

func TestRuleUnboundedQuery_PassesWithLimit(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	posts, _ := models.QueryPost().Limit(20).All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index"},
		},
	}
	findings := ruleUnboundedQuery(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with Limit(), got %d", len(findings))
	}
}

func TestRuleUnboundedQuery_PassesWithPaginate(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	posts, _ := models.QueryPost().Paginate(page, 20).All()
	_ = posts
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Index": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts", ControllerType: "PostController", MethodName: "Index"},
		},
	}
	findings := ruleUnboundedQuery(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with Paginate(), got %d", len(findings))
	}
}

func TestRuleUnboundedQuery_SkipsNonQueryAll(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post, _ := models.QueryPost().First()
	_ = post
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Show": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/posts/:id", ControllerType: "PostController", MethodName: "Show"},
		},
	}
	findings := ruleUnboundedQuery(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for First() query, got %d", len(findings))
	}
}

// ---- Rule: public_projection ----

func TestRulePublicProjection_FlagsBareModelVar(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	user, _ := models.QueryUser().First()
	return ctx.JSON(200, user)
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"UserController.Show": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/users/:id", ControllerType: "UserController", MethodName: "Show", Middleware: []string{}},
		},
	}
	findings := rulePublicProjection(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "public_projection" {
		t.Errorf("wrong rule: %s", findings[0].Rule)
	}
}

func TestRulePublicProjection_PassesWithPublic(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	user, _ := models.QueryUser().First()
	return ctx.JSON(200, user.Public())
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"UserController.Show": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/users/:id", ControllerType: "UserController", MethodName: "Show", Middleware: []string{}},
		},
	}
	findings := rulePublicProjection(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings with .Public(), got %d", len(findings))
	}
}

func TestRulePublicProjection_SkipsAuthRoutes(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	user, _ := models.QueryUser().First()
	return ctx.JSON(200, user)
}`
	m := method(t, src)
	ctx := &AnalysisContext{
		Config: defaultConfig(),
		Methods: map[string]*ControllerMethod{
			"UserController.Show": m,
		},
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/profile", ControllerType: "UserController", MethodName: "Show", Middleware: []string{"Auth"}},
		},
	}
	findings := rulePublicProjection(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for authenticated routes, got %d", len(findings))
	}
}

// ---- AllRules ----

func TestAllRules_ContainsExpectedRules(t *testing.T) {
	rules := AllRules()
	expected := []string{
		"no_printf", "no_recover", "ownership_scoping", "read_scoping",
		"enum_validation", "uuid_error_handling", "public_projection",
		"required_fields", "unbounded_query", "rate_limit_auth",
		"auth_without_middleware", "param_mismatch", "csrf_missing",
		"sensitive_field_encryption", "public_sensitive_conflict",
	}
	for _, name := range expected {
		if _, ok := rules[name]; !ok {
			t.Errorf("expected rule %q in AllRules()", name)
		}
	}
}

// ---- isSensitiveColumn ----

func TestIsSensitiveColumn_ExactNames(t *testing.T) {
	exactNames := []string{
		"password", "email", "ssn", "access_token", "api_key",
		"session_key", "refresh_token", "secret", "private_key",
		"credit_card", "card_number", "cvv", "pin", "date_of_birth",
		"phone", "phone_number",
	}
	for _, name := range exactNames {
		if !isSensitiveColumn(name) {
			t.Errorf("expected %q to be sensitive", name)
		}
	}
}

func TestIsSensitiveColumn_NotSensitive(t *testing.T) {
	names := []string{"title", "body", "created_at", "name", "count"}
	for _, name := range names {
		if isSensitiveColumn(name) {
			t.Errorf("expected %q to NOT be sensitive", name)
		}
	}
}

// ---- CallChain ----

func TestCallChain_Names(t *testing.T) {
	chain := CallChain{
		Segments: []ChainSegment{
			{Name: "models"},
			{Name: "QueryPost"},
			{Name: "First"},
		},
	}
	names := chain.Names()
	if len(names) != 3 || names[0] != "models" || names[1] != "QueryPost" || names[2] != "First" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestCallChain_HasSegment(t *testing.T) {
	chain := CallChain{
		Segments: []ChainSegment{
			{Name: "QueryPost"},
			{Name: "WhereID"},
		},
	}
	if !chain.HasSegment("QueryPost") {
		t.Error("expected HasSegment to find QueryPost")
	}
	if chain.HasSegment("NotHere") {
		t.Error("expected HasSegment to not find NotHere")
	}
}

func TestCallChain_HasSegmentWithAuthArg_DirectCall(t *testing.T) {
	src := `package test
import "models"
func F() {
	models.QueryPost().WhereUserID(ctx.Auth().UserID).First()
}`
	body, fset, _ := parseFunc(t, src)
	chains := ExtractCallChains(body, fset)

	found := false
	for _, c := range chains {
		if c.HasSegmentWithAuthArg("Where") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth arg to be found in Where segment")
	}
}

// ---- ExtractCallChains ----

func TestExtractCallChains_SimpleChain(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	models.QueryPost().WhereID(id).First()
}`
	m := method(t, src)
	chains := ExtractCallChains(m.Body, m.Fset)
	if !chainHasSegment(chains, "QueryPost") {
		t.Error("expected chain with QueryPost")
	}
}

func TestExtractCallChains_SingleCallNotIncluded(t *testing.T) {
	src := `package controllers
func Handler() {
	x()
}`
	m := method(t, src)
	// Single-segment chains (len==1) should NOT be included
	chains := ExtractCallChains(m.Body, m.Fset)
	for _, c := range chains {
		if len(c.Segments) <= 1 {
			t.Error("single-segment chains should be excluded")
		}
	}
}
