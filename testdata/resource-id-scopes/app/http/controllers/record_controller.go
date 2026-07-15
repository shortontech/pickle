package controllers

// Show demonstrates the required parse, authorize, and scoped-query sequence.
func (c RecordController) Show(ctx *pickle.Context) pickle.Response {
	parts, err := ctx.ParamResourceIDParts("record_id")
	if err != nil {
		return ctx.BadRequest("invalid record id")
	}
	if parts.ScopeID != ctx.Auth().OrganizationID {
		return ctx.Forbidden("record belongs to another organization")
	}
	record, err := models.QueryRecord().
		WhereOrganizationID(parts.ScopeID).
		WhereRecordID(parts.RecordID).
		First()
	if err != nil {
		return ctx.NotFound("record not found")
	}
	return ctx.JSON(200, record)
}

// UnsafeShow is intentionally rejected by resource_id_unscoped.
func (c RecordController) UnsafeShow(ctx *pickle.Context) pickle.Response {
	parts, err := ctx.ParamResourceIDParts("record_id")
	if err != nil {
		return ctx.BadRequest("invalid record id")
	}
	record, err := models.QueryRecord().WhereRecordID(parts.RecordID).First()
	if err != nil {
		return ctx.NotFound("record not found")
	}
	return ctx.JSON(200, record)
}
