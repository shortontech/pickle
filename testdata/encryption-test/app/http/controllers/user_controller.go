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
	users, err := models.QueryUser().AnyOwner().Limit(100).All()
	if err != nil {
		return ctx.Error(err)
	}
	return ctx.JSON(200, models.PublicUsers(users))
}

func (c UserController) Show(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid id"})
	}

	user, err := models.QueryUser().
		AnyOwner().
		WhereID(id).
		First()
	if err != nil {
		return ctx.NotFound("user not found")
	}
	return ctx.JSON(200, user.Public())
}
