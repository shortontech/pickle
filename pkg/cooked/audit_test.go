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
