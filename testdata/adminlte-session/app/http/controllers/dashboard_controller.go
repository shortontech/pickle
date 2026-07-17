package controllers

import pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"

type DashboardController struct{ pickle.Controller }

func (DashboardController) Index(ctx *pickle.Context) pickle.Response {
	data := pickle.DashboardData{Authenticated: true}
	data.Page.Title = "Warehouse dashboard"
	data.Page.Heading = "Dashboard"
	data.User.Name = ctx.Auth().UserID
	data.Metrics = append(data.Metrics, struct {
		Label string
		Value string
	}{Label: "Open orders", Value: "12"})
	return pickle.Dashboard(ctx, data)
}
