package generator

import (
	"testing"
)

func TestParseRoutesBasic(t *testing.T) {
	src := []byte(`package app

var API = pickle.Routes(func(r *pickle.Router) {
	r.Get("/health", HealthController{}.Check)
	r.Post("/login", AuthController{}.Login)
})
`)

	routes, err := ParseRoutes("routes.go", src)
	if err != nil {
		t.Fatalf("ParseRoutes: %v", err)
	}

	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}

	assertRoute(t, routes[0], "GET", "/health", "HealthController", "Check")
	assertRoute(t, routes[1], "POST", "/login", "AuthController", "Login")
}

func TestParseRoutesGroup(t *testing.T) {
	src := []byte(`package app

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Get("/users", UserController{}.Index)
		r.Group("/posts", Auth, func(r *pickle.Router) {
			r.Get("/", PostController{}.Index)
			r.Post("/", PostController{}.Store)
		})
	})
})
`)

	routes, err := ParseRoutes("routes.go", src)
	if err != nil {
		t.Fatalf("ParseRoutes: %v", err)
	}

	if len(routes) != 3 {
		t.Fatalf("got %d routes, want 3", len(routes))
	}

	assertRoute(t, routes[0], "GET", "/api/users", "UserController", "Index")
	assertRoute(t, routes[1], "GET", "/api/posts/", "PostController", "Index")
	assertRoute(t, routes[2], "POST", "/api/posts/", "PostController", "Store")

	// Nested group should inherit middleware
	if len(routes[1].Middleware) != 1 || routes[1].Middleware[0] != "Auth" {
		t.Errorf("route[1] middleware = %v, want [Auth]", routes[1].Middleware)
	}
}

func TestParseRoutesResource(t *testing.T) {
	src := []byte(`package app

var API = pickle.Routes(func(r *pickle.Router) {
	r.Resource("/users", UserController{})
})
`)

	routes, err := ParseRoutes("routes.go", src)
	if err != nil {
		t.Fatalf("ParseRoutes: %v", err)
	}

	if len(routes) != 5 {
		t.Fatalf("got %d routes, want 5 (CRUD)", len(routes))
	}

	assertRoute(t, routes[0], "GET", "/users", "UserController", "Index")
	assertRoute(t, routes[1], "GET", "/users/:id", "UserController", "Show")
	assertRoute(t, routes[2], "POST", "/users", "UserController", "Store")
	assertRoute(t, routes[3], "PUT", "/users/:id", "UserController", "Update")
	assertRoute(t, routes[4], "DELETE", "/users/:id", "UserController", "Destroy")
}

func TestParseRoutesPerRouteMiddleware(t *testing.T) {
	src := []byte(`package app

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", Auth, func(r *pickle.Router) {
		r.Post("/transfers", TransferController{}.Store, RequireRole("admin", "finance"))
	})
})
`)

	routes, err := ParseRoutes("routes.go", src)
	if err != nil {
		t.Fatalf("ParseRoutes: %v", err)
	}

	if len(routes) != 1 {
		t.Fatalf("got %d routes, want 1", len(routes))
	}

	r := routes[0]
	if len(r.Middleware) != 2 {
		t.Fatalf("middleware count = %d, want 2", len(r.Middleware))
	}
	if r.Middleware[0] != "Auth" {
		t.Errorf("middleware[0] = %q, want Auth", r.Middleware[0])
	}
	if r.Middleware[1] != `RequireRole("admin", "finance")` {
		t.Errorf("middleware[1] = %q, want RequireRole(\"admin\", \"finance\")", r.Middleware[1])
	}
}

func TestParseRoutesBasicCrud(t *testing.T) {
	src := []byte(`package basiccrud

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Resource("/users", UserController{})

		r.Group("/posts", Auth, func(r *pickle.Router) {
			r.Get("/", PostController{}.Index)
			r.Get("/:id", PostController{}.Show)
			r.Post("/", PostController{}.Store)
			r.Put("/:id", PostController{}.Update)
			r.Delete("/:id", PostController{}.Destroy)
		})
	})
})
`)

	routes, err := ParseRoutes("routes.go", src)
	if err != nil {
		t.Fatalf("ParseRoutes: %v", err)
	}

	// 5 resource routes + 5 explicit routes = 10
	if len(routes) != 10 {
		t.Fatalf("got %d routes, want 10", len(routes))
	}

	// User resource routes should have no middleware
	for _, r := range routes[:5] {
		if len(r.Middleware) != 0 {
			t.Errorf("user route %s %s has middleware %v, want none", r.Method, r.Path, r.Middleware)
		}
	}

	// Post routes should have Auth middleware
	for _, r := range routes[5:] {
		if len(r.Middleware) != 1 || r.Middleware[0] != "Auth" {
			t.Errorf("post route %s %s middleware = %v, want [Auth]", r.Method, r.Path, r.Middleware)
		}
	}
}

func assertRoute(t *testing.T, r ParsedRoute, method, path, controller, action string) {
	t.Helper()
	if r.Method != method {
		t.Errorf("method = %q, want %q", r.Method, method)
	}
	if r.Path != path {
		t.Errorf("path = %q, want %q", r.Path, path)
	}
	if r.Controller != controller {
		t.Errorf("controller = %q, want %q", r.Controller, controller)
	}
	if r.Action != action {
		t.Errorf("action = %q, want %q", r.Action, action)
	}
}
