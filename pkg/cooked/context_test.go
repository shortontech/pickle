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
	if got := ctx.Param("missing"); got != "" {
		t.Errorf("Param(missing) = %q, want empty", got)
	}
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

	if ctx.Auth() != nil {
		t.Error("Auth() should be nil before SetAuth")
	}

	ctx.SetAuth(&AuthInfo{UserID: "u1", Role: "admin"})
	if ctx.Auth().UserID != "u1" || ctx.Auth().Role != "admin" {
		t.Errorf("Auth() = %+v, want UserID=u1 Role=admin", ctx.Auth())
	}

	// SetAuth with raw claims
	ctx.SetAuth("raw-claims")
	if ctx.Auth().Claims != "raw-claims" {
		t.Errorf("Auth().Claims = %v, want raw-claims", ctx.Auth().Claims)
	}
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
