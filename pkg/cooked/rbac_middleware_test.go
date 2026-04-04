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

func TestLoadRolesPopulatesContext(t *testing.T) {
	old := RoleLoaderFunc
	defer func() { RoleLoaderFunc = old }()

	RoleLoaderFunc = func(userID string) ([]RoleInfo, error) {
		if userID != "u1" {
			t.Errorf("expected userID u1, got %s", userID)
		}
		return []RoleInfo{
			{Slug: "editor", Manages: false},
			{Slug: "admin", Manages: true},
		}, nil
	}

	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetAuth(&AuthInfo{UserID: "u1"})

	resp := LoadRoles(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !ctx.HasRole("editor") || !ctx.HasRole("admin") {
		t.Error("expected roles to be populated")
	}
	if !ctx.IsAdmin() {
		t.Error("expected IsAdmin true")
	}
}

func TestLoadRolesWithoutAuth(t *testing.T) {
	old := RoleLoaderFunc
	defer func() { RoleLoaderFunc = old }()

	RoleLoaderFunc = func(userID string) ([]RoleInfo, error) {
		t.Fatal("should not be called without auth")
		return nil, nil
	}

	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	// No SetAuth — simulates missing Auth middleware

	resp := LoadRoles(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestLoadRolesNoLoaderFunc(t *testing.T) {
	old := RoleLoaderFunc
	defer func() { RoleLoaderFunc = old }()

	RoleLoaderFunc = nil

	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	ctx.SetAuth(&AuthInfo{UserID: "u1"})

	resp := LoadRoles(ctx, func() Response {
		return ctx.JSON(200, "ok")
	})

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 with nil loader, got %d", resp.StatusCode)
	}
}
