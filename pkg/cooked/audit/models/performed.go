//go:build ignore

package models

import (
	"database/sql"
	"github.com/google/uuid"
)

// Performed records a successful action execution in the user_actions table.
// Called within the same transaction as the action itself — both succeed or
// both roll back. The *sql.Tx is used directly to insert the audit row.
func Performed(ctx *Context, actionTypeID int, resourceID uuid.UUID, resourceVersionID *uuid.UUID, roleID *uuid.UUID, tx *sql.Tx) error {
	ipAddr := ctx.IP()
	reqID := ctx.RequestID()
	ua := &UserAction{
		UserID:            uuid.MustParse(ctx.Auth().UserID),
		ActionTypeID:      actionTypeID,
		ResourceID:        resourceID,
		ResourceVersionID: resourceVersionID,
		RoleID:            roleID,
		IPAddress:         &ipAddr,
		RequestID:         &reqID,
	}
	_, err := tx.Exec(
		`INSERT INTO user_actions (user_id, action_type_id, resource_id, resource_version_id, role_id, ip_address, request_id) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ua.UserID, ua.ActionTypeID, ua.ResourceID, ua.ResourceVersionID, ua.RoleID, ua.IPAddress, ua.RequestID,
	)
	return err
}
