package controllers

import pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"

type DashboardController struct{ pickle.Controller }

func (DashboardController) Asset(ctx *pickle.Context) pickle.Response {
	return pickle.PickleAsset(ctx)
}

func (DashboardController) Index(ctx *pickle.Context) pickle.Response {
	data := pickle.DashboardData{}
	data.Page.Title = "Warehouse dashboard"
	data.Page.Heading = "Dashboard"
	data.User.Name = ctx.Auth().UserID
	data.CsrfToken, _ = ctx.Cookie("csrf_token")
	data.Orders.Value = "12"
	data.Shipments.Value = "8"
	data.Inventory.Value = "5"
	data.Suppliers.Value = "2"
	return pickle.Dashboard(ctx, data)
}
