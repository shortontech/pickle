package squeeze

import (
	"testing"
)

// csrfControllerSrc is a minimal controller that calls session.Create.
const csrfControllerSrc = `package controllers
import "session"
func Login() {
	session.Create(ctx, userID, role)
}
`

// csrfMethodsWithSession returns a Methods map containing a controller that uses session.Create.
func csrfMethodsWithSession(t *testing.T) map[string]*ControllerMethod {
	t.Helper()
	body, fset, _ := parseFunc(t, csrfControllerSrc)
	return map[string]*ControllerMethod{
		"AuthController.Login": {Body: body, Fset: fset, File: "controllers/auth.go", Line: 3},
	}
}

func TestCsrfMissing_FlagsPostWithoutCSRF(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/register", Middleware: []string{}, File: "routes/web.go", Line: 10},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "csrf_missing" {
		t.Errorf("rule = %q, want csrf_missing", findings[0].Rule)
	}
}

func TestCsrfMissing_SkipsGET(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes: []AnalyzedRoute{
			{Method: "GET", Path: "/dashboard", Middleware: []string{}, File: "routes/web.go", Line: 5},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for GET route, got %d", len(findings))
	}
}

func TestCsrfMissing_SkipsWhenNoSessionUsage(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{}, // no session.Create calls
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/users", Middleware: []string{}, File: "routes/web.go", Line: 10},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings without session usage, got %d", len(findings))
	}
}

func TestCsrfMissing_PassesWithCSRFMiddleware(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/transfers", Middleware: []string{"Auth", "CSRF"}, File: "routes/web.go", Line: 15},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings with CSRF middleware, got %d", len(findings))
	}
}

func TestCsrfMissing_CustomCSRFMiddlewareName(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Config: SqueezeConfig{
			Middleware: MiddlewareConfig{
				CSRF: []string{"VerifyCSRF"},
			},
		},
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/transfers", Middleware: []string{"Auth", "VerifyCSRF"}, File: "routes/web.go", Line: 15},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings with custom CSRF middleware, got %d", len(findings))
	}
}

func TestCsrfMissing_AllStateChangingMethods(t *testing.T) {
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/a", File: "routes/web.go", Line: 1},
			{Method: "PUT", Path: "/b", File: "routes/web.go", Line: 2},
			{Method: "PATCH", Path: "/c", File: "routes/web.go", Line: 3},
			{Method: "DELETE", Path: "/d", File: "routes/web.go", Line: 4},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 4 {
		t.Fatalf("expected 4 findings, got %d", len(findings))
	}
}

func TestCsrfMissing_DetectsSessionCreateInFuncRegistry(t *testing.T) {
	serviceSrc := `package services
import "session"
func CreateUserSession() {
	session.Create(ctx, userID, role)
}
`
	body, fset, _ := parseFunc(t, serviceSrc)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{}, // no session.Create in controllers
		FuncRegistry: FuncRegistry{
			"services.CreateUserSession": &ParsedFunc{Body: body, Fset: fset, Params: nil},
		},
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/login", File: "routes/web.go", Line: 5},
		},
	}

	findings := ruleCsrfMissing(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding when session.Create is in func registry, got %d", len(findings))
	}
}
