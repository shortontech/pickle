package cooked

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestAuditPerformed_NilContext(t *testing.T) {
	// Must not panic with nil context.
	AuditPerformed(nil, "view", "User", "abc-123")
}

func TestAuditPerformed_NoAuth(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	ctx := NewContext(w, r)
	AuditPerformed(ctx, "view", "User", 42)
}

func TestAuditPerformed_WithAuth(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	ctx := NewContext(w, r)
	ctx.SetAuth(&AuthInfo{UserID: "user-1", Role: "admin"})
	ctx.SetRoles([]RoleInfo{{Slug: "admin", Manages: true}, {Slug: "editor"}})
	AuditPerformed(ctx, "ban_user", "User", "target-99")
}

func TestAuditDenied_NilContext(t *testing.T) {
	AuditDenied(nil, "delete", "Post", nil, "insufficient permissions")
}

func TestAuditDenied_WithAuth(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	ctx := NewContext(w, r)
	ctx.SetAuth(&AuthInfo{UserID: "user-2"})
	AuditDenied(ctx, "delete", "Post", "post-5", "missing role")
}

func TestAuditFailed_NilContext(t *testing.T) {
	AuditFailed(nil, "create", "Transfer", nil, errors.New("db connection lost"))
}

func TestAuditFailed_WithAuth(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", nil)
	ctx := NewContext(w, r)
	ctx.SetAuth(&AuthInfo{UserID: "user-3"})
	AuditFailed(ctx, "create", "Transfer", "txn-1", errors.New("timeout"))
}

func TestAuditPerformed_NilResourceID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	ctx := NewContext(w, r)
	AuditPerformed(ctx, "list", "User", nil)
}

func TestAuditPerformed_IncludesIP(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.1:4321"
	ctx := NewContext(w, r)
	// Should not panic; IP is extracted from RemoteAddr.
	AuditPerformed(ctx, "view", "User", 1)
}

func TestAuditPerformed_IncludesRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-ID", "req-abc-123")
	ctx := NewContext(w, r)
	AuditPerformed(ctx, "view", "User", 1)
}

func TestAuditPerformed_XForwardedFor(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.RemoteAddr = "10.0.0.1:1234"
	ctx := NewContext(w, r)
	AuditPerformed(ctx, "view", "User", 1)
}

func TestAuditOverride_Performed(t *testing.T) {
	var called bool
	var gotAction, gotModel, gotExtra string
	var gotResourceID any

	old := OnAuditPerformed
	OnAuditPerformed = func(ctx *Context, action, model string, resourceID any, extra string) {
		called = true
		gotAction = action
		gotModel = model
		gotResourceID = resourceID
		gotExtra = extra
	}
	defer func() { OnAuditPerformed = old }()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	ctx := NewContext(w, r)
	AuditPerformed(ctx, "ban", "User", "u-1")

	if !called {
		t.Fatal("override was not called")
	}
	if gotAction != "ban" || gotModel != "User" || gotResourceID != "u-1" || gotExtra != "" {
		t.Errorf("unexpected args: action=%q model=%q resourceID=%v extra=%q", gotAction, gotModel, gotResourceID, gotExtra)
	}
}

func TestAuditOverride_Denied(t *testing.T) {
	var gotReason string
	old := OnAuditDenied
	OnAuditDenied = func(ctx *Context, action, model string, resourceID any, extra string) {
		gotReason = extra
	}
	defer func() { OnAuditDenied = old }()

	AuditDenied(nil, "delete", "Post", nil, "no_permission")
	if gotReason != "no_permission" {
		t.Errorf("expected reason 'no_permission', got %q", gotReason)
	}
}

func TestAuditOverride_Failed(t *testing.T) {
	var gotExtra string
	old := OnAuditFailed
	OnAuditFailed = func(ctx *Context, action, model string, resourceID any, extra string) {
		gotExtra = extra
	}
	defer func() { OnAuditFailed = old }()

	AuditFailed(nil, "create", "Transfer", nil, errors.New("db down"))
	if gotExtra != "db down" {
		t.Errorf("expected error text 'db down', got %q", gotExtra)
	}
}
