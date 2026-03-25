package cooked

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireRolePass(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{{Slug: "admin", Manages: true}})

	mw := RequireRole("admin", "editor")
	resp := mw(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRequireRoleFail(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{{Slug: "viewer"}})

	mw := RequireRole("admin", "editor")
	resp := mw(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestRequireRoleMultipleAllowed(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{{Slug: "editor"}})

	mw := RequireRole("admin", "editor")
	resp := mw(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for matching role, got %d", resp.StatusCode)
	}
}

func TestRequireRoleNoRoles(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	// No SetRoles called

	mw := RequireRole("admin")
	resp := mw(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 with no roles, got %d", resp.StatusCode)
	}
}

func TestRequireAdminPass(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{{Slug: "admin", Manages: true}})

	resp := RequireAdmin(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRequireAdminFail(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetRoles([]RoleInfo{{Slug: "editor", Manages: false}})

	resp := RequireAdmin(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}
