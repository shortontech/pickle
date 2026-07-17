package cooked

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

var authenticateHTTPPolicy func(*http.Request) (any, *AuthInfo, error)

// RegisterHTTPPolicyAuthenticator installs the generated sealed auth-to-model
// bridge without introducing an HTTP/auth/models import cycle.
func RegisterHTTPPolicyAuthenticator(authenticator func(*http.Request) (any, *AuthInfo, error)) {
	authenticateHTTPPolicy = authenticator
}

// MiddlewareFunc is the signature for middleware functions.
type MiddlewareFunc func(ctx *Context, next func() Response) Response

// HandlerFunc is a resolved handler that takes a Context and returns a Response.
type HandlerFunc func(ctx *Context) Response

// Route describes a single registered route.
type Route struct {
	NameValue  string
	Method     string
	Path       string
	Handler    HandlerFunc
	Middleware []MiddlewareFunc
}

// Name assigns a stable application name to a route.
func (r *Route) Name(name string) *Route {
	if r == nil {
		panic("pickle: cannot name a nil route")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		panic("pickle: route name must not be empty")
	}
	r.NameValue = name
	return r
}

type RouteParams map[string]any

type ResourceRoutes struct {
	router *Router
	start  int
}

type RouteGroup struct{ router *Router }

// Name prefixes every named route in the group, including nested groups.
func (g *RouteGroup) Name(prefix string) *RouteGroup {
	if g == nil || g.router == nil {
		panic("pickle: invalid route group")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		panic("pickle: route group name prefix must not be empty")
	}
	g.router.prefixRouteNames(prefix)
	return g
}

func (r *Router) prefixRouteNames(prefix string) {
	for i := range r.routes {
		if r.routes[i].NameValue != "" {
			r.routes[i].NameValue = prefix + r.routes[i].NameValue
		}
	}
	for _, group := range r.groups {
		group.prefixRouteNames(prefix)
	}
}

func (rr *ResourceRoutes) Names(prefix string) *ResourceRoutes {
	if rr == nil || rr.router == nil || len(rr.router.routes) < rr.start+5 {
		panic("pickle: invalid resource route set")
	}
	for i, suffix := range []string{"index", "show", "store", "update", "destroy"} {
		rr.router.routes[rr.start+i].Name(prefix + "." + suffix)
	}
	return rr
}

// ErrorReporter is called when ctx.Error() handles an unrecoverable error or
// when panic recovery catches a panic. Use it to report to Sentry, Datadog, etc.
type ErrorReporter func(ctx *Context, err error)

// Router collects route definitions. It is a descriptor, not a runtime router.
type Router struct {
	prefix     string
	middleware []MiddlewareFunc
	routes     []Route
	groups     []*Router
	onError    ErrorReporter
}

// OnError registers a callback that is invoked for panics recovered during
// request handling. Use this to wire in external error reporting (Sentry, etc.).
func (r *Router) OnError(fn ErrorReporter) {
	r.onError = fn
}

// OnRateLimit registers a callback that is invoked on every rate limit check
// (both IP and auth layers). Use this for metrics and alerting.
func (r *Router) OnRateLimit(fn func(ctx *Context, event RateLimitEvent)) {
	rateLimitCallback = fn
}

// Routes creates a new Router by invoking the given configuration function.
func Routes(fn func(r *Router)) *Router {
	r := &Router{}
	fn(r)
	return r
}

func (r *Router) addRoute(method, path string, handler HandlerFunc, mw []any) *Route {
	if r == nil {
		return nil
	}
	r.routes = append(r.routes, Route{
		Method:     method,
		Path:       path,
		Handler:    handler,
		Middleware: resolveMiddleware(mw),
	})
	return &r.routes[len(r.routes)-1]
}

// resolveMiddleware converts a slice of any (MiddlewareFunc or MiddlewareProvider)
// into a slice of MiddlewareFunc. This runs at route registration time, not per-request.
func resolveMiddleware(mw []any) []MiddlewareFunc {
	resolved := make([]MiddlewareFunc, 0, len(mw))
	for _, m := range mw {
		switch v := m.(type) {
		case MiddlewareFunc:
			resolved = append(resolved, v)
		case func(*Context, func() Response) Response:
			resolved = append(resolved, MiddlewareFunc(v))
		case MiddlewareProvider:
			resolved = append(resolved, v.Middleware())
		default:
			panic(fmt.Sprintf("pickle: invalid middleware type %T — must be MiddlewareFunc or MiddlewareProvider", m))
		}
	}
	return resolved
}

// Get registers a GET route.
func (r *Router) Get(path string, handler HandlerFunc, mw ...any) *Route {
	return r.addRoute("GET", path, handler, mw)
}

// Post registers a POST route.
func (r *Router) Post(path string, handler HandlerFunc, mw ...any) *Route {
	return r.addRoute("POST", path, handler, mw)
}

// Put registers a PUT route.
func (r *Router) Put(path string, handler HandlerFunc, mw ...any) *Route {
	return r.addRoute("PUT", path, handler, mw)
}

// Patch registers a PATCH route.
func (r *Router) Patch(path string, handler HandlerFunc, mw ...any) *Route {
	return r.addRoute("PATCH", path, handler, mw)
}

// Delete registers a DELETE route.
func (r *Router) Delete(path string, handler HandlerFunc, mw ...any) *Route {
	return r.addRoute("DELETE", path, handler, mw)
}

// Group creates a sub-router with a shared prefix and optional middleware.
func (r *Router) Group(prefix string, body func(*Router), mw ...any) *RouteGroup {
	g := &Router{prefix: prefix, middleware: resolveMiddleware(mw)}
	body(g)
	r.groups = append(r.groups, g)
	return &RouteGroup{router: g}
}

// Resource registers standard CRUD routes for a controller.
type ResourceController interface {
	Index(*Context) Response
	Show(*Context) Response
	Store(*Context) Response
	Update(*Context) Response
	Destroy(*Context) Response
}

func (r *Router) Resource(prefix string, c ResourceController, mw ...any) *ResourceRoutes {
	start := len(r.routes)
	r.addRoute("GET", prefix, c.Index, mw)
	r.addRoute("GET", prefix+"/:id", c.Show, mw)
	r.addRoute("POST", prefix, c.Store, mw)
	r.addRoute("PUT", prefix+"/:id", c.Update, mw)
	r.addRoute("DELETE", prefix+"/:id", c.Destroy, mw)
	return &ResourceRoutes{router: r, start: start}
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
			NameValue:  route.NameValue,
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

func (r *Router) namedRoutes() map[string]Route {
	named := map[string]Route{}
	for _, route := range r.AllRoutes() {
		if route.NameValue == "" {
			continue
		}
		if _, exists := named[route.NameValue]; exists {
			panic("pickle: duplicate route name: " + route.NameValue)
		}
		named[route.NameValue] = route
	}
	return named
}

// URL builds a path for a named route.
func (r *Router) URL(name string, params RouteParams) string {
	route, ok := r.namedRoutes()[name]
	if !ok {
		panic("pickle: unknown route name: " + name)
	}
	used := map[string]bool{}
	path := paramPattern.ReplaceAllStringFunc(route.Path, func(token string) string {
		key := strings.TrimPrefix(token, ":")
		value, exists := params[key]
		if !exists {
			panic("pickle: route " + name + " requires parameter " + key)
		}
		used[key] = true
		return url.PathEscape(fmt.Sprint(value))
	})
	for key := range params {
		if !used[key] {
			panic("pickle: route " + name + " does not define parameter " + key)
		}
	}
	return path
}

var paramPattern = regexp.MustCompile(`:(\w+)`)

// RegisterRoutes wires all routes onto the given ServeMux.
// Also registers Pickle's internal operations endpoints (/pickle/*).
func (r *Router) RegisterRoutes(mux *http.ServeMux) {
	// Register Pickle's internal operations endpoints
	RegisterPickleEndpoints(mux)

	_ = r.namedRoutes()
	registered := map[string]bool{}
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

		onError := r.onError
		handler := func(w http.ResponseWriter, req *http.Request) {
			// Framework-level rate limiting — runs before everything else.
			resp, ipRLHeaders := checkRateLimit(req)
			if resp != nil {
				resp.Write(w)
				return
			}

			ctx := NewContext(w, req)
			ctx.router = r
			ctx.routeName = route.NameValue
			if authenticateHTTPPolicy != nil {
				policyContext, authInfo, err := authenticateHTTPPolicy(req)
				if err != nil {
					ctx.Unauthorized("invalid credentials").Write(w)
					return
				}
				ctx.SetPolicyContext(policyContext)
				if authInfo != nil {
					ctx.SetAuth(authInfo)
				}
			}

			defer func() {
				if rv := recover(); rv != nil {
					err, ok := rv.(error)
					if !ok {
						err = fmt.Errorf("%v", rv)
					}
					log.Printf("panic: %v\n%s", err, debug.Stack())
					if onError != nil {
						onError(ctx, err)
					}
					resp := Response{
						StatusCode: http.StatusInternalServerError,
						Body:       map[string]string{"error": "internal server error"},
						Headers:    map[string]string{"Content-Type": "application/json"},
					}
					resp.Write(w)
				}
			}()

			for _, name := range params {
				ctx.SetParam(name, req.PathValue(name))
			}

			var mw []MiddlewareFunc
			if len(route.Middleware) > 0 {
				mw = route.Middleware
			}

			result := RunMiddleware(ctx, mw, func() Response {
				return route.Handler(ctx)
			})
			// Attach IP-layer rate limit headers to the response.
			for k, v := range ipRLHeaders {
				if result.Headers == nil {
					result.Headers = make(map[string]string)
				}
				result.Headers[k] = v
			}
			result.Write(w)
		}

		if registered[pattern] {
			panic("pickle: duplicate route registered: " + pattern)
		}
		registered[pattern] = true
		mux.HandleFunc(pattern, handler)

		// Register the opposite slash variant to prevent ServeMux 301 redirects
		// that strip headers (e.g. Authorization). Skip paths ending in path params.
		if !strings.HasSuffix(goPath, "}") {
			var alt string
			if strings.HasSuffix(goPath, "/") {
				trimmed := strings.TrimRight(goPath, "/")
				if trimmed != "" {
					alt = route.Method + " " + trimmed
				}
			} else {
				alt = route.Method + " " + goPath + "/"
			}
			if alt != "" && !registered[alt] {
				registered[alt] = true
				mux.HandleFunc(alt, handler)
			}
		}
	}
}

// Convenience: register on http.DefaultServeMux
func (r *Router) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}
