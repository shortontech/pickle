package controllers

import pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"

type TransactionController struct {
	pickle.Controller
}

func (c TransactionController) Index(ctx *pickle.Context) pickle.Response {
	// TODO: list resources
	return ctx.JSON(200, map[string]string{"status": "ok"})
}

func (c TransactionController) Show(ctx *pickle.Context) pickle.Response {
	// TODO: show resource by ctx.Param("id")
	return ctx.JSON(200, nil)
}

func (c TransactionController) Store(ctx *pickle.Context) pickle.Response {
	// TODO: create resource
	return ctx.JSON(201, nil)
}

func (c TransactionController) Update(ctx *pickle.Context) pickle.Response {
	// TODO: update resource
	return ctx.JSON(200, nil)
}

func (c TransactionController) Destroy(ctx *pickle.Context) pickle.Response {
	// TODO: delete resource
	return ctx.NoContent()
}
