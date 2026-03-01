package cooked

import "testing"

func noop(ctx *Context) Response { return Response{} }

func TestRoutesBasic(t *testing.T) {
	r := Routes(func(r *Router) {
		r.Get("/health", noop)
		r.Post("/users", noop)
	})

	routes := r.AllRoutes()
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}
	if routes[0].Method != "GET" || routes[0].Path != "/health" {
		t.Errorf("route[0] = %s %s, want GET /health", routes[0].Method, routes[0].Path)
	}
	if routes[1].Method != "POST" || routes[1].Path != "/users" {
		t.Errorf("route[1] = %s %s, want POST /users", routes[1].Method, routes[1].Path)
	}
}

func TestRoutesGroup(t *testing.T) {
	mw := MiddlewareFunc(func(ctx *Context, next func() Response) Response { return next() })

	r := Routes(func(r *Router) {
		r.Group("/api", func(r *Router) {
			r.Get("/users", noop)
			r.Group("/admin", func(r *Router) {
				r.Delete("/users/:id", noop)
			})
		}, mw)
	})

	routes := r.AllRoutes()
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}
	if routes[0].Path != "/api/users" {
		t.Errorf("route[0].Path = %q, want /api/users", routes[0].Path)
	}
	if routes[1].Path != "/api/admin/users/:id" {
		t.Errorf("route[1].Path = %q, want /api/admin/users/:id", routes[1].Path)
	}
	if len(routes[0].Middleware) != 1 {
		t.Errorf("route[0] middleware count = %d, want 1", len(routes[0].Middleware))
	}
	if len(routes[1].Middleware) != 1 {
		t.Errorf("route[1] middleware count = %d, want 1 (inherited)", len(routes[1].Middleware))
	}
}

func TestRoutesPerRouteMiddleware(t *testing.T) {
	mw1 := MiddlewareFunc(func(ctx *Context, next func() Response) Response { return next() })
	mw2 := MiddlewareFunc(func(ctx *Context, next func() Response) Response { return next() })

	r := Routes(func(r *Router) {
		r.Group("/api", func(r *Router) {
			r.Post("/transfers", noop, mw2)
		}, mw1)
	})

	routes := r.AllRoutes()
	if len(routes[0].Middleware) != 2 {
		t.Errorf("middleware count = %d, want 2 (group + per-route)", len(routes[0].Middleware))
	}
}
