package controllers

import (
	pickle "monorepo/services/api/http"
	"monorepo/services/api/http/requests"
	"monorepo/app/models"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type OrderController struct {
	pickle.Controller
}

func (c OrderController) Index(ctx *pickle.Context) pickle.Response {
	orders, err := models.QueryOrder().
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		All()
	if err != nil {
		return ctx.Error(err)
	}
	return ctx.JSON(200, orders)
}

func (c OrderController) Store(ctx *pickle.Context) pickle.Response {
	req, bindErr := requests.BindCreateOrderRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	order := &models.Order{
		UserID:   uuid.MustParse(ctx.Auth().UserID),
		Total:    decimal.RequireFromString(req.Total),
		Currency: req.Currency,
		Status:   "pending",
	}

	if err := models.QueryOrder().Create(order); err != nil {
		return ctx.Error(err)
	}
	return ctx.JSON(201, order)
}
