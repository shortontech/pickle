package cooked

import (
	"fmt"
	"log"
)

// AuditPerformed logs a successful action. Called after a gate check passes
// and the action is executed.
func AuditPerformed(ctx *Context, action, model string, resourceID any) {
	userID := auditUserID(ctx)
	roles := auditRoles(ctx)
	log.Printf("audit.performed user_id=%s roles=%v action=%s model=%s resource_id=%v",
		userID, roles, action, model, resourceID)
}

// AuditDenied logs a denied action. Called when a gate check fails.
func AuditDenied(ctx *Context, action, model string, resourceID any, reason string) {
	userID := auditUserID(ctx)
	roles := auditRoles(ctx)
	log.Printf("audit.denied user_id=%s roles=%v action=%s model=%s resource_id=%v reason=%s",
		userID, roles, action, model, resourceID, reason)
}

// AuditFailed logs a failed action. Called when an action errors after authorisation.
func AuditFailed(ctx *Context, action, model string, resourceID any, err error) {
	userID := auditUserID(ctx)
	roles := auditRoles(ctx)
	log.Printf("audit.failed user_id=%s roles=%v action=%s model=%s resource_id=%v error=%v",
		userID, roles, action, model, resourceID, err)
}

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
