package controllers

import (
	pickle "github.com/shortontech/pickle/testdata/encryption-test/app/http"
	"github.com/shortontech/pickle/testdata/encryption-test/app/models"
	"github.com/google/uuid"
)

type UserController struct {
	pickle.Controller
}

func (c UserController) Index(ctx *pickle.Context) pickle.Response {
	users, err := models.QueryUser().All()
	if err != nil {
		return ctx.Error(err)
	}
	return ctx.JSON(200, users)
}

func (c UserController) Show(ctx *pickle.Context) pickle.Response {
	user, err := models.QueryUser().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		First()
	if err != nil {
		return ctx.NotFound("user not found")
	}
	return ctx.JSON(200, user)
}
