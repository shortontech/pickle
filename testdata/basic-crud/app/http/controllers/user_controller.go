package controllers

import (
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/requests"
	"github.com/shortontech/pickle/testdata/basic-crud/app/models"

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

func (c UserController) Store(ctx *pickle.Context) pickle.Response {
	req, bindErr := requests.BindCreateUserRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	user := &models.User{
		Name:     req.Name,
		Email:    req.Email,
		PasswordHash: HashPassword(req.Password),
	}

	if err := models.QueryUser().Create(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, user)
}

func (c UserController) Update(ctx *pickle.Context) pickle.Response {
	req, bindErr := requests.BindUpdateUserRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	user, err := models.QueryUser().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		First()

	if err != nil {
		return ctx.NotFound("user not found")
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Email != "" {
		user.Email = req.Email
	}

	if err := models.QueryUser().Update(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, user)
}

func (c UserController) Destroy(ctx *pickle.Context) pickle.Response {
	user, err := models.QueryUser().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		First()

	if err != nil {
		return ctx.NotFound("user not found")
	}

	if err := models.QueryUser().Delete(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.NoContent()
}
