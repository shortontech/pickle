package cooked

import (
	"net/http"
	"regexp"
	"strings"
)

// MiddlewareFunc is the signature for middleware functions.
type MiddlewareFunc func(ctx *Context, next func() Response) Response

// HandlerFunc is a resolved handler that takes a Context and returns a Response.
type HandlerFunc func(ctx *Context) Response

// Route describes a single registered route.
type Route struct {
	Method     string
	Path       string
	Handler    HandlerFunc
	Middleware []MiddlewareFunc
}

// Router collects route definitions. It is a descriptor, not a runtime router.
type Router struct {
	prefix     string
	middleware []MiddlewareFunc
	routes     []Route
	groups     []*Router
}

// Routes creates a new Router by invoking the given configuration function.
func Routes(fn func(r *Router)) *Router {
	r := &Router{}
	fn(r)
	return r
}

func (r *Router) addRoute(method, path string, handler HandlerFunc, mw []MiddlewareFunc) {
	r.routes = append(r.routes, Route{
		Method:     method,
		Path:       path,
		Handler:    handler,
		Middleware: mw,
	})
}

// Get registers a GET route.
func (r *Router) Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) {
	r.addRoute("GET", path, handler, mw)
}

// Post registers a POST route.
func (r *Router) Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) {
	r.addRoute("POST", path, handler, mw)
}

// Put registers a PUT route.
func (r *Router) Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) {
	r.addRoute("PUT", path, handler, mw)
}

// Patch registers a PATCH route.
func (r *Router) Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) {
	r.addRoute("PATCH", path, handler, mw)
}

// Delete registers a DELETE route.
func (r *Router) Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) {
	r.addRoute("DELETE", path, handler, mw)
}

// Group creates a sub-router with a shared prefix and optional middleware.
// The last func(*Router) argument is the group body; all other arguments
// before it are treated as MiddlewareFunc.
func (r *Router) Group(prefix string, args ...any) {
	g := &Router{prefix: prefix}

	for _, arg := range args {
		switch v := arg.(type) {
		case MiddlewareFunc:
			g.middleware = append(g.middleware, v)
		case func(*Context, func() Response) Response:
			g.middleware = append(g.middleware, v)
		case func(r *Router):
			v(g)
		}
	}

	r.groups = append(r.groups, g)
}

// Resource registers standard CRUD routes for a controller.
type ResourceController interface {
	Index(*Context) Response
	Show(*Context) Response
	Store(*Context) Response
	Update(*Context) Response
	Destroy(*Context) Response
}

func (r *Router) Resource(prefix string, c ResourceController, mw ...MiddlewareFunc) {
	r.addRoute("GET", prefix, c.Index, mw)
	r.addRoute("GET", prefix+"/:id", c.Show, mw)
	r.addRoute("POST", prefix, c.Store, mw)
	r.addRoute("PUT", prefix+"/:id", c.Update, mw)
	r.addRoute("DELETE", prefix+"/:id", c.Destroy, mw)
}

// AllRoutes returns a flattened list of all routes with prefixes and
// middleware fully resolved.
func (r *Router) AllRoutes() []Route {
	return r.collectRoutes("", nil)
}

func (r *Router) collectRoutes(parentPrefix string, parentMW []MiddlewareFunc) []Route {
	fullPrefix := parentPrefix + r.prefix
	combinedMW := append(append([]MiddlewareFunc{}, parentMW...), r.middleware...)

	var routes []Route
	for _, route := range r.routes {
		resolved := Route{
			Method:     route.Method,
			Path:       fullPrefix + route.Path,
			Handler:    route.Handler,
			Middleware: append(append([]MiddlewareFunc{}, combinedMW...), route.Middleware...),
		}
		routes = append(routes, resolved)
	}

	for _, g := range r.groups {
		routes = append(routes, g.collectRoutes(fullPrefix, combinedMW)...)
	}

	return routes
}

var paramPattern = regexp.MustCompile(`:(\w+)`)

// RegisterRoutes wires all routes onto the given ServeMux.
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	for _, route := range r.AllRoutes() {
		route := route // capture

		// Convert :param to Go 1.22+ {param}
		goPath := paramPattern.ReplaceAllString(route.Path, "{${1}}")

		// Extract param names
		var params []string
		for _, match := range paramPattern.FindAllStringSubmatch(route.Path, -1) {
			params = append(params, match[1])
		}

		pattern := route.Method + " " + goPath

		handler := func(w http.ResponseWriter, req *http.Request) {
			ctx := NewContext(w, req)
			for _, name := range params {
				ctx.SetParam(name, req.PathValue(name))
			}

			var mw []MiddlewareFunc
			if len(route.Middleware) > 0 {
				mw = route.Middleware
			}

			resp := RunMiddleware(ctx, mw, func() Response {
				return route.Handler(ctx)
			})
			resp.Write(w)
		}

		mux.HandleFunc(pattern, handler)

		// Register the opposite slash variant to prevent ServeMux 301 redirects
		// that strip headers (e.g. Authorization). Skip paths ending in path params.
		if !strings.HasSuffix(goPath, "}") {
			if strings.HasSuffix(goPath, "/") {
				trimmed := strings.TrimRight(goPath, "/")
				if trimmed != "" {
					mux.HandleFunc(route.Method+" "+trimmed, handler)
				}
			} else {
				mux.HandleFunc(route.Method+" "+goPath+"/", handler)
			}
		}
	}
}

// Convenience: register on http.DefaultServeMux
func (r *Router) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)
	return http.ListenAndServe(addr, mux)
}

// trimTrailingSlash normalizes paths.
func trimTrailingSlash(s string) string {
	if len(s) > 1 {
		return strings.TrimRight(s, "/")
	}
	return s
}
