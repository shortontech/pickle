package squeeze

import (
	"go/parser"
	"os"
	"path/filepath"
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

// mwName parses a middleware expression and returns the classified name.
func mwName(t *testing.T, src string) string {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parsing %q: %v", src, err)
	}
	return extractMiddlewareName(expr)
}

func TestExtractMiddlewareName_UnwrapsMiddlewareFunc(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// pickle-qualified MiddlewareFunc conversion — the form the router forces.
		{"pickle.MiddlewareFunc(session.CSRF)", "CSRF"},
		// Bare (unqualified) MiddlewareFunc conversion.
		{"MiddlewareFunc(session.CSRF)", "CSRF"},
		// Selector middleware.
		{"middleware.Auth", "Auth"},
		// Constructor middleware must still classify by constructor name.
		{"pickle.RateLimit(1, 5)", "RateLimit"},
		{"middleware.RequireRole(\"admin\")", "RequireRole"},
	}
	for _, tc := range cases {
		if got := mwName(t, tc.src); got != tc.want {
			t.Errorf("extractMiddlewareName(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestCsrfMissing_PassesWithMiddlewareFuncCSRF(t *testing.T) {
	// pickle.MiddlewareFunc(session.CSRF) applied to a group must protect the route.
	dir := t.TempDir()
	routesSrc := `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
	"myapp/session"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/secure", func(r *pickle.Router) {
		r.Post("/transfers", controllers.TransferController{}.Store)
	}, pickle.MiddlewareFunc(session.CSRF))
})
`
	if err := os.WriteFile(filepath.Join(dir, "web.go"), []byte(routesSrc), 0644); err != nil {
		t.Fatalf("writing routes: %v", err)
	}

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("ParseRoutes: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if len(routes[0].Middleware) != 1 || routes[0].Middleware[0] != "CSRF" {
		t.Fatalf("expected middleware [CSRF], got %v", routes[0].Middleware)
	}

	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes:  routes,
	}
	if findings := ruleCsrfMissing(ctx); len(findings) != 0 {
		t.Fatalf("expected 0 findings with MiddlewareFunc(session.CSRF), got %d", len(findings))
	}
}

func TestCsrfMissing_PassesWithBareMiddlewareFuncCSRF(t *testing.T) {
	// Bare (unqualified) MiddlewareFunc(session.CSRF) must also protect the route.
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/transfers", Middleware: []string{mwName(t, "MiddlewareFunc(session.CSRF)")}, File: "routes/web.go", Line: 15},
		},
	}
	if findings := ruleCsrfMissing(ctx); len(findings) != 0 {
		t.Fatalf("expected 0 findings with bare MiddlewareFunc(session.CSRF), got %d", len(findings))
	}
}

func TestCsrfMissing_FlagsWithNonCSRFMiddlewareFunc(t *testing.T) {
	// A route whose only middleware is a non-CSRF constructor still fires csrf_missing.
	ctx := &AnalysisContext{
		Methods: csrfMethodsWithSession(t),
		Routes: []AnalyzedRoute{
			{Method: "POST", Path: "/transfers", Middleware: []string{mwName(t, "pickle.RateLimit(1, 5)")}, File: "routes/web.go", Line: 15},
		},
	}
	findings := ruleCsrfMissing(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding with only RateLimit middleware, got %d", len(findings))
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
