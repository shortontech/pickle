package cooked

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContextParam(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetParam("id", "abc-123")

	if got := ctx.Param("id"); got != "abc-123" {
		t.Errorf("Param(id) = %q, want %q", got, "abc-123")
	}
	// Param() should panic for missing params
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Param(missing) should panic for undefined param")
			}
		}()
		ctx.Param("missing")
	}()
}

func TestContextQuery(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=3&q=hello", nil)
	ctx := NewContext(httptest.NewRecorder(), r)

	if got := ctx.Query("page"); got != "3" {
		t.Errorf("Query(page) = %q, want %q", got, "3")
	}
	if got := ctx.Query("missing"); got != "" {
		t.Errorf("Query(missing) = %q, want empty", got)
	}
}

func TestContextBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer tok123", "tok123"},
		{"bearer tok123", ""},
		{"Basic abc", ""},
		{"", ""},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		if tt.header != "" {
			r.Header.Set("Authorization", tt.header)
		}
		ctx := NewContext(httptest.NewRecorder(), r)
		if got := ctx.BearerToken(); got != tt.want {
			t.Errorf("BearerToken() with %q = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestContextAuth(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	// Auth() should panic before SetAuth (no auth middleware ran)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Auth() should panic before SetAuth")
			}
		}()
		ctx.Auth()
	}()

	ctx.SetAuth(&AuthInfo{UserID: "u1", Role: "admin"})
	if ctx.Auth().UserID != "u1" || ctx.Auth().Role != "admin" {
		t.Errorf("Auth() = %+v, want UserID=u1 Role=admin", ctx.Auth())
	}

	// SetAuth with non-AuthInfo panics
	defer func() {
		r := recover()
		if r == nil {
			t.Error("SetAuth with non-*AuthInfo should panic")
		}
	}()
	ctx.SetAuth("raw-claims")
}

func TestContextResponseHelpers(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	r := ctx.JSON(201, map[string]string{"id": "1"})
	if r.StatusCode != 201 {
		t.Errorf("JSON status = %d, want 201", r.StatusCode)
	}

	r = ctx.NoContent()
	if r.StatusCode != http.StatusNoContent {
		t.Errorf("NoContent status = %d, want 204", r.StatusCode)
	}

	r = ctx.NotFound("gone")
	if r.StatusCode != http.StatusNotFound {
		t.Errorf("NotFound status = %d, want 404", r.StatusCode)
	}

	r = ctx.Unauthorized("nope")
	if r.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unauthorized status = %d, want 401", r.StatusCode)
	}

	r = ctx.Forbidden("denied")
	if r.StatusCode != http.StatusForbidden {
		t.Errorf("Forbidden status = %d, want 403", r.StatusCode)
	}
}

func TestContextRoles(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	// Before SetRoles
	if ctx.Role() != "" {
		t.Error("expected empty role before SetRoles")
	}
	if len(ctx.Roles()) != 0 {
		t.Error("expected empty roles before SetRoles")
	}
	if ctx.HasRole("admin") {
		t.Error("expected HasRole false before SetRoles")
	}
	if ctx.IsAdmin() {
		t.Error("expected IsAdmin false before SetRoles")
	}

	// Set roles
	ctx.SetRoles([]RoleInfo{
		{Slug: "editor", Manages: false},
		{Slug: "admin", Manages: true},
	})

	if ctx.Role() != "editor" {
		t.Errorf("Role() = %q, want 'editor'", ctx.Role())
	}
	if len(ctx.Roles()) != 2 {
		t.Errorf("Roles() len = %d, want 2", len(ctx.Roles()))
	}
	if !ctx.HasRole("admin") {
		t.Error("expected HasRole('admin') to be true")
	}
	if !ctx.HasRole("editor") {
		t.Error("expected HasRole('editor') to be true")
	}
	if ctx.HasRole("viewer") {
		t.Error("expected HasRole('viewer') to be false")
	}
	if !ctx.IsAdmin() {
		t.Error("expected IsAdmin true when admin role has Manages")
	}
}

func TestContextHasAnyRole(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{{Slug: "editor"}})

	if !ctx.HasAnyRole("admin", "editor") {
		t.Error("expected HasAnyRole true with partial match")
	}
	if ctx.HasAnyRole("admin", "viewer") {
		t.Error("expected HasAnyRole false with no match")
	}
	if ctx.HasAnyRole() {
		t.Error("expected HasAnyRole false with empty args")
	}
}

func TestContextIsAdminFalse(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{
		{Slug: "editor", Manages: false},
		{Slug: "viewer", Manages: false},
	})
	if ctx.IsAdmin() {
		t.Error("expected IsAdmin false when no role has Manages")
	}
}
