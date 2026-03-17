package squeeze

import (
	"os"
	"path/filepath"
	"testing"
)

// writeRouteFile writes a Go file with route definitions to a temp directory.
func writeRouteFile(t *testing.T, dir, filename, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0644); err != nil {
		t.Fatalf("writing route file: %v", err)
	}
}

func TestParseRoutes_SimpleRoute(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Get("/users", controllers.UserController{}.Index)
	r.Post("/users", controllers.UserController{}.Store)
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	var get, post *AnalyzedRoute
	for i := range routes {
		if routes[i].Method == "GET" {
			get = &routes[i]
		} else if routes[i].Method == "POST" {
			post = &routes[i]
		}
	}
	if get == nil || get.Path != "/users" {
		t.Errorf("expected GET /users route, got %v", get)
	}
	if post == nil || post.Path != "/users" {
		t.Errorf("expected POST /users route, got %v", post)
	}
	if get.ControllerType != "UserController" {
		t.Errorf("expected UserController, got %q", get.ControllerType)
	}
	if get.MethodName != "Index" {
		t.Errorf("expected Index, got %q", get.MethodName)
	}
}

func TestParseRoutes_GroupWithMiddleware(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
	"myapp/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", middleware.Auth, func(r *pickle.Router) {
		r.Get("/posts", controllers.PostController{}.Index)
		r.Delete("/posts/:id", controllers.PostController{}.Destroy)
	})
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	for _, r := range routes {
		if r.Path != "/api/posts" && r.Path != "/api/posts/:id" {
			t.Errorf("unexpected path: %s", r.Path)
		}
		found := false
		for _, mw := range r.Middleware {
			if mw == "Auth" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected Auth middleware on route %s %s", r.Method, r.Path)
		}
	}
}

func TestParseRoutes_NestedGroups(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
	"myapp/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", middleware.RateLimit, func(r *pickle.Router) {
		r.Group("/admin", middleware.RequireAdmin, func(r *pickle.Router) {
			r.Get("/users", controllers.UserController{}.Index)
		})
	})
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	r := routes[0]
	if r.Path != "/api/admin/users" {
		t.Errorf("expected /api/admin/users, got %s", r.Path)
	}
	if len(r.Middleware) < 2 {
		t.Errorf("expected both RateLimit and RequireAdmin middleware, got %v", r.Middleware)
	}
}

func TestParseRoutes_Resource(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Resource("/posts", controllers.PostController{})
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Resource generates: Index, Show, Store, Update, Destroy
	if len(routes) != 5 {
		t.Fatalf("expected 5 resource routes, got %d", len(routes))
	}

	methods := make(map[string]bool)
	for _, r := range routes {
		methods[r.MethodName] = true
	}
	for _, expected := range []string{"Index", "Show", "Store", "Update", "Destroy"} {
		if !methods[expected] {
			t.Errorf("expected resource method %q", expected)
		}
	}
}

func TestParseRoutes_MissingDir(t *testing.T) {
	routes, err := ParseRoutes("/nonexistent/routes/dir")
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if routes != nil {
		t.Error("expected nil routes for missing dir")
	}
}

func TestParseRoutes_AllHTTPMethods(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Get("/a", controllers.C{}.A)
	r.Post("/b", controllers.C{}.B)
	r.Put("/c", controllers.C{}.C)
	r.Patch("/d", controllers.C{}.D)
	r.Delete("/e", controllers.C{}.E)
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 5 {
		t.Fatalf("expected 5 routes, got %d", len(routes))
	}

	methodSet := make(map[string]bool)
	for _, r := range routes {
		methodSet[r.Method] = true
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if !methodSet[m] {
			t.Errorf("missing method %s", m)
		}
	}
}

func TestParseRoutes_SkipsGenFiles(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web_gen.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Get("/generated", controllers.C{}.Index)
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// _gen.go files should be skipped
	if len(routes) != 0 {
		t.Errorf("expected 0 routes from _gen.go file, got %d", len(routes))
	}
}

func TestParseRoutes_PerRouteMiddleware(t *testing.T) {
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
	"myapp/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Post("/login", controllers.AuthController{}.Login, middleware.RateLimit)
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	found := false
	for _, mw := range routes[0].Middleware {
		if mw == "RateLimit" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RateLimit middleware on per-route middleware, got %v", routes[0].Middleware)
	}
}

func TestExtractMiddlewareName_Parameterized(t *testing.T) {
	// parseGroup handles middleware.RequireRole("admin") — selector expression
	dir := t.TempDir()
	writeRouteFile(t, dir, "web.go", `package routes

import (
	pickle "myapp/app/http"
	"myapp/app/http/controllers"
	"myapp/app/http/middleware"
)

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/admin", middleware.RequireRole("admin"), func(r *pickle.Router) {
		r.Get("/users", controllers.UserController{}.Index)
	})
})
`)

	routes, err := ParseRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	found := false
	for _, mw := range routes[0].Middleware {
		if mw == "RequireRole" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RequireRole middleware, got %v", routes[0].Middleware)
	}
}
