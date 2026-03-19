package controllers

import pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"

type AccountController struct {
	pickle.Controller
}

func (c AccountController) Index(ctx *pickle.Context) pickle.Response {
	// TODO: list resources
	return ctx.JSON(200, map[string]string{"status": "ok"})
}

func (c AccountController) Show(ctx *pickle.Context) pickle.Response {
	// TODO: show resource by ctx.Param("id")
	return ctx.JSON(200, nil)
}

func (c AccountController) Store(ctx *pickle.Context) pickle.Response {
	// TODO: create resource
	return ctx.JSON(201, nil)
}

func (c AccountController) Update(ctx *pickle.Context) pickle.Response {
	// TODO: update resource
	return ctx.JSON(200, nil)
}

func (c AccountController) Destroy(ctx *pickle.Context) pickle.Response {
	// TODO: delete resource
	return ctx.NoContent()
}
