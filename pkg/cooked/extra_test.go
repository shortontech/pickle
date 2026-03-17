package cooked

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// --- Context additional coverage ---

func TestContextRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	ctx := NewContext(httptest.NewRecorder(), r)
	if ctx.Request() != r {
		t.Error("Request() should return the original *http.Request")
	}
}

func TestContextResponseWriter(t *testing.T) {
	w := httptest.NewRecorder()
	ctx := NewContext(w, httptest.NewRequest("GET", "/", nil))
	if ctx.ResponseWriter() != w {
		t.Error("ResponseWriter() should return the original http.ResponseWriter")
	}
}

func TestContextParamUUIDValid(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	id := uuid.New()
	ctx.SetParam("id", id.String())
	got, err := ctx.ParamUUID("id")
	if err != nil {
		t.Errorf("ParamUUID valid = %v, want no error", err)
	}
	if got != id {
		t.Errorf("ParamUUID = %v, want %v", got, id)
	}
}

func TestContextParamUUIDInvalid(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetParam("id", "not-a-uuid")
	_, err := ctx.ParamUUID("id")
	if err == nil {
		t.Error("ParamUUID with invalid UUID should return error")
	}
}

func TestContextCookiePresent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
	ctx := NewContext(httptest.NewRecorder(), r)
	val, err := ctx.Cookie("session")
	if err != nil {
		t.Errorf("Cookie() error = %v, want nil", err)
	}
	if val != "abc123" {
		t.Errorf("Cookie() = %q, want abc123", val)
	}
}

func TestContextCookieMissing(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	_, err := ctx.Cookie("missing")
	if err == nil {
		t.Error("Cookie() missing cookie should return error")
	}
}

func TestContextError(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	resp := ctx.Error(errors.New("something went wrong"))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Error status = %d, want 500", resp.StatusCode)
	}
}

func TestContextBadRequest(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	resp := ctx.BadRequest("invalid input")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("BadRequest status = %d, want 400", resp.StatusCode)
	}
	body, ok := resp.Body.(map[string]string)
	if !ok || body["error"] != "invalid input" {
		t.Errorf("BadRequest body = %v, want error:invalid input", resp.Body)
	}
}

func TestContextResourceNoAuth(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	q := &mockResourceQuery{result: map[string]string{"id": "1"}}
	resp := ctx.Resource(q)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Resource status = %d, want 200", resp.StatusCode)
	}
	if q.gotOwnerID != "" {
		t.Errorf("Resource ownerID = %q, want empty (no auth)", q.gotOwnerID)
	}
}

func TestContextResourceWithAuth(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetAuth(&AuthInfo{UserID: "user-42"})
	q := &mockResourceQuery{result: map[string]string{"id": "1"}}
	resp := ctx.Resource(q)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Resource status = %d, want 200", resp.StatusCode)
	}
	if q.gotOwnerID != "user-42" {
		t.Errorf("Resource ownerID = %q, want user-42", q.gotOwnerID)
	}
}

func TestContextResourceNotFound(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	q := &mockResourceQuery{err: fmt.Errorf("sql: no rows in result set")}
	resp := ctx.Resource(q)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Resource not found status = %d, want 404", resp.StatusCode)
	}
}

func TestContextResourceError(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	q := &mockResourceQuery{err: errors.New("db error")}
	resp := ctx.Resource(q)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Resource db error status = %d, want 500", resp.StatusCode)
	}
}

func TestContextResourcesNoAuth(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	q := &mockResourceListQuery{result: []string{"a", "b"}}
	resp := ctx.Resources(q)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Resources status = %d, want 200", resp.StatusCode)
	}
}

func TestContextResourcesError(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	q := &mockResourceListQuery{err: errors.New("db error")}
	resp := ctx.Resources(q)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Resources error status = %d, want 500", resp.StatusCode)
	}
}

type mockResourceQuery struct {
	result     any
	err        error
	gotOwnerID string
}

func (m *mockResourceQuery) FetchResource(ownerID string) (any, error) {
	m.gotOwnerID = ownerID
	return m.result, m.err
}

type mockResourceListQuery struct {
	result     any
	err        error
	gotOwnerID string
}

func (m *mockResourceListQuery) FetchResources(ownerID string) (any, error) {
	m.gotOwnerID = ownerID
	return m.result, m.err
}

// --- Response additional coverage ---

func TestResponseWithCookie(t *testing.T) {
	r := Response{StatusCode: 200}
	cookie := &http.Cookie{Name: "token", Value: "abc"}
	r2 := r.WithCookie(cookie)
	if len(r2.Cookies) != 1 || r2.Cookies[0].Name != "token" {
		t.Errorf("WithCookie = %v, want token cookie", r2.Cookies)
	}
	// Original is unchanged
	if len(r.Cookies) != 0 {
		t.Error("WithCookie should not modify original response")
	}
}

func TestResponseWriteWithCookie(t *testing.T) {
	w := httptest.NewRecorder()
	r := Response{
		StatusCode: 200,
		Body:       map[string]string{"ok": "true"},
		Cookies:    []*http.Cookie{{Name: "session", Value: "xyz"}},
	}
	r.Write(w)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Error("response cookies not written")
	}
	if cookies[0].Name != "session" || cookies[0].Value != "xyz" {
		t.Errorf("cookie = %+v, want session=xyz", cookies[0])
	}
}

func TestResponseWriteZeroStatusWithBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := Response{Body: "hello"}
	r.Write(w)
	if w.Code != 200 {
		t.Errorf("zero status + body = %d, want 200", w.Code)
	}
}

func TestResponseWriteZeroStatusNoBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := Response{}
	r.Write(w)
	if w.Code != http.StatusNoContent {
		t.Errorf("zero status + no body = %d, want 204", w.Code)
	}
}

func TestResponseHeaderNilMap(t *testing.T) {
	r := Response{StatusCode: 200}
	// Headers is nil initially
	r2 := r.Header("X-Test", "value")
	if r2.Headers["X-Test"] != "value" {
		t.Errorf("Header on nil map = %q, want value", r2.Headers["X-Test"])
	}
}

// --- Router additional coverage ---

func TestRouterPutPatch(t *testing.T) {
	r := Routes(func(r *Router) {
		r.Put("/users/:id", noop)
		r.Patch("/users/:id", noop)
	})
	routes := r.AllRoutes()
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2", len(routes))
	}
	if routes[0].Method != "PUT" {
		t.Errorf("route[0].Method = %q, want PUT", routes[0].Method)
	}
	if routes[1].Method != "PATCH" {
		t.Errorf("route[1].Method = %q, want PATCH", routes[1].Method)
	}
}

func TestRouterResource(t *testing.T) {
	ctrl := &mockResourceController{}
	r := Routes(func(r *Router) {
		r.Resource("/posts", ctrl)
	})
	routes := r.AllRoutes()
	if len(routes) != 5 {
		t.Fatalf("Resource routes = %d, want 5", len(routes))
	}
	methods := map[string]bool{}
	for _, route := range routes {
		methods[route.Method] = true
	}
	for _, m := range []string{"GET", "POST", "PUT", "DELETE"} {
		if !methods[m] {
			t.Errorf("Resource missing method %q", m)
		}
	}
}

func TestRouterResourceWithMiddleware(t *testing.T) {
	ctrl := &mockResourceController{}
	mw := MiddlewareFunc(func(ctx *Context, next func() Response) Response { return next() })
	r := Routes(func(r *Router) {
		r.Resource("/items", ctrl, mw)
	})
	routes := r.AllRoutes()
	for _, route := range routes {
		if len(route.Middleware) != 1 {
			t.Errorf("Resource route %s %s middleware count = %d, want 1", route.Method, route.Path, len(route.Middleware))
		}
	}
}

func TestRouterRegisterRoutes(t *testing.T) {
	r := Routes(func(r *Router) {
		r.Get("/health", func(ctx *Context) Response {
			return ctx.JSON(200, map[string]string{"status": "ok"})
		})
		r.Post("/echo", func(ctx *Context) Response {
			return ctx.JSON(201, nil)
		})
	})
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)

	// Test GET /health
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("GET /health = %d, want 200", w.Code)
	}
}

func TestRouterRegisterRoutesWithParam(t *testing.T) {
	r := Routes(func(r *Router) {
		r.Get("/users/:id", func(ctx *Context) Response {
			return ctx.JSON(200, map[string]string{"id": ctx.Param("id")})
		})
	})
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/users/42", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("GET /users/42 = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "42") {
		t.Errorf("response body = %q, want to contain 42", w.Body.String())
	}
}

func TestRouterRegisterRoutesDuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("duplicate route registration should panic")
		}
	}()
	r := Routes(func(r *Router) {
		r.Get("/dup", noop)
		r.Get("/dup", noop)
	})
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)
}

func TestRouterRegisterRoutesTrailingSlash(t *testing.T) {
	r := Routes(func(r *Router) {
		r.Get("/api/health", func(ctx *Context) Response {
			return ctx.JSON(200, nil)
		})
	})
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)

	// Both with and without trailing slash should work (no 301)
	for _, path := range []string{"/api/health", "/api/health/"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		// Should not redirect (301 would strip headers)
		if w.Code == http.StatusMovedPermanently {
			t.Errorf("GET %s returned 301, should not redirect", path)
		}
	}
}

func TestRouterMiddlewareRunOnRequest(t *testing.T) {
	mwRan := false
	mw := MiddlewareFunc(func(ctx *Context, next func() Response) Response {
		mwRan = true
		return next()
	})
	r := Routes(func(r *Router) {
		r.Get("/protected", noop, mw)
	})
	mux := http.NewServeMux()
	r.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if !mwRan {
		t.Error("middleware should have run on request")
	}
}

func TestResponseWriteMarshalError(t *testing.T) {
	w := httptest.NewRecorder()
	// func values can't be JSON-marshaled
	r := Response{StatusCode: 200, Body: func() {}}
	r.Write(w)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("marshal error status = %d, want 500", w.Code)
	}
}

func TestContextResourcesWithAuth(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetAuth(&AuthInfo{UserID: "user-99"})
	q := &mockResourceListQuery{result: []string{"x"}}
	resp := ctx.Resources(q)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Resources with auth status = %d, want 200", resp.StatusCode)
	}
	if q.gotOwnerID != "user-99" {
		t.Errorf("Resources ownerID = %q, want user-99", q.gotOwnerID)
	}
}

type mockResourceController struct{}

func (m *mockResourceController) Index(ctx *Context) Response   { return Response{StatusCode: 200} }
func (m *mockResourceController) Show(ctx *Context) Response    { return Response{StatusCode: 200} }
func (m *mockResourceController) Store(ctx *Context) Response   { return Response{StatusCode: 201} }
func (m *mockResourceController) Update(ctx *Context) Response  { return Response{StatusCode: 200} }
func (m *mockResourceController) Destroy(ctx *Context) Response { return Response{StatusCode: 204} }
