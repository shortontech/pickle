package cooked

import (
	"fmt"
	"log"
	"net"
)

// AuditFunc is the signature for audit handler functions. Users can override
// the default audit behaviour by assigning custom functions to
// OnAuditPerformed, OnAuditDenied, and OnAuditFailed.
type AuditFunc func(ctx *Context, action, model string, resourceID any, extra string)

// OnAuditPerformed is called for every successful action execution.
// Replace this variable to send audit events to a database, external service, etc.
var OnAuditPerformed AuditFunc

// OnAuditDenied is called when a gate denies an action.
// Replace this variable to customise denied-action logging.
var OnAuditDenied AuditFunc

// OnAuditFailed is called when an action errors after authorisation.
// Replace this variable to customise failed-action logging.
var OnAuditFailed AuditFunc

// AuditPerformed logs a successful action. Called after a gate check passes
// and the action is executed.
func AuditPerformed(ctx *Context, action, model string, resourceID any) {
	if OnAuditPerformed != nil {
		OnAuditPerformed(ctx, action, model, resourceID, "")
		return
	}
	userID := auditUserID(ctx)
	roles := auditRoles(ctx)
	ip := auditIP(ctx)
	reqID := auditRequestID(ctx)
	log.Printf("audit.performed user_id=%s roles=%v action=%s model=%s resource_id=%v ip=%s request_id=%s",
		userID, roles, action, model, resourceID, ip, reqID)
}

// AuditDenied logs a denied action. Called when a gate check fails.
func AuditDenied(ctx *Context, action, model string, resourceID any, reason string) {
	if OnAuditDenied != nil {
		OnAuditDenied(ctx, action, model, resourceID, reason)
		return
	}
	userID := auditUserID(ctx)
	roles := auditRoles(ctx)
	ip := auditIP(ctx)
	reqID := auditRequestID(ctx)
	log.Printf("audit.denied user_id=%s roles=%v action=%s model=%s resource_id=%v reason=%s ip=%s request_id=%s",
		userID, roles, action, model, resourceID, reason, ip, reqID)
}

// AuditFailed logs a failed action. Called when an action errors after authorisation.
func AuditFailed(ctx *Context, action, model string, resourceID any, err error) {
	if OnAuditFailed != nil {
		OnAuditFailed(ctx, action, model, resourceID, err.Error())
		return
	}
	userID := auditUserID(ctx)
	roles := auditRoles(ctx)
	ip := auditIP(ctx)
	reqID := auditRequestID(ctx)
	log.Printf("audit.failed user_id=%s roles=%v action=%s model=%s resource_id=%v error=%v ip=%s request_id=%s",
		userID, roles, action, model, resourceID, err, ip, reqID)
}

// AuditHook is the function type for audit trail integration.
// The generator wires this to the database-backed audit package when
// audit tables are present. Default: no-op (log-only audit above handles it).
var AuditHook func(ctx *Context, actionTypeID int, resourceID, resourceVersionID, roleID interface{}) error

func auditUserID(ctx *Context) string {
	if ctx == nil || ctx.auth == nil {
		return ""
	}
	return ctx.auth.UserID
}

func auditRoles(ctx *Context) string {
	if ctx == nil {
		return "[]"
	}
	return fmt.Sprintf("%v", ctx.Roles())
}

func auditIP(ctx *Context) string {
	if ctx == nil || ctx.request == nil {
		return ""
	}
	// Check X-Forwarded-For first, then X-Real-IP, then RemoteAddr.
	if xff := ctx.request.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := ctx.request.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(ctx.request.RemoteAddr)
	if err != nil {
		return ctx.request.RemoteAddr
	}
	return host
}

func auditRequestID(ctx *Context) string {
	if ctx == nil || ctx.request == nil {
		return ""
	}
	if id := ctx.request.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return ctx.request.Header.Get("X-Request-Id")
}
