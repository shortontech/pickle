package cooked

// MiddlewareFunc is the signature for middleware functions.
type MiddlewareFunc func(ctx *Context, next func() Response) Response

// Route describes a single registered route.
type Route struct {
	Method     string
	Path       string
	Handler    any
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

func (r *Router) addRoute(method, path string, handler any, mw []MiddlewareFunc) {
	r.routes = append(r.routes, Route{
		Method:     method,
		Path:       path,
		Handler:    handler,
		Middleware: mw,
	})
}

// Get registers a GET route.
func (r *Router) Get(path string, handler any, mw ...MiddlewareFunc) {
	r.addRoute("GET", path, handler, mw)
}

// Post registers a POST route.
func (r *Router) Post(path string, handler any, mw ...MiddlewareFunc) {
	r.addRoute("POST", path, handler, mw)
}

// Put registers a PUT route.
func (r *Router) Put(path string, handler any, mw ...MiddlewareFunc) {
	r.addRoute("PUT", path, handler, mw)
}

// Patch registers a PATCH route.
func (r *Router) Patch(path string, handler any, mw ...MiddlewareFunc) {
	r.addRoute("PATCH", path, handler, mw)
}

// Delete registers a DELETE route.
func (r *Router) Delete(path string, handler any, mw ...MiddlewareFunc) {
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
		case func(r *Router):
			v(g)
		}
	}

	r.groups = append(r.groups, g)
}

// Resource registers standard CRUD routes for a controller.
// It looks for Index, Show, Store, Update, and Destroy methods via
// interface checks on the controller.
func (r *Router) Resource(prefix string, controller any, mw ...MiddlewareFunc) {
	type indexer interface{ Index(*Context) Response }
	type shower interface{ Show(*Context) Response }
	type destroyer interface{ Destroy(*Context) Response }

	if c, ok := controller.(indexer); ok {
		r.addRoute("GET", prefix, c.Index, mw)
	}
	if c, ok := controller.(shower); ok {
		r.addRoute("GET", prefix+"/:id", c.Show, mw)
	}
	// Store and Update use `any` handler since they take request structs —
	// the generator resolves the actual method signature.
	r.addRoute("POST", prefix, controller, mw)
	r.addRoute("PUT", prefix+"/:id", controller, mw)
	if c, ok := controller.(destroyer); ok {
		r.addRoute("DELETE", prefix+"/:id", c.Destroy, mw)
	}
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
