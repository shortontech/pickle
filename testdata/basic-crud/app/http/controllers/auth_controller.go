package controllers

import (
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/auth"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/auth/jwt"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/requests"
	"github.com/shortontech/pickle/testdata/basic-crud/app/models"
)

type AuthController struct {
	pickle.Controller
}

func (c AuthController) Login(ctx *pickle.Context) pickle.Response {
	req, bindErr := requests.BindLoginRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	user, err := models.QueryUser().WhereEmail(req.Email).First()
	if err != nil {
		return ctx.Unauthorized("invalid credentials")
	}

	if !CheckPassword(user.PasswordHash, req.Password) {
		return ctx.Unauthorized("invalid credentials")
	}

	driver := auth.Driver("jwt").(*jwt.Driver)
	token, err := driver.SignToken(jwt.Claims{
		Subject: user.ID.String(),
		Role:    "user",
	})
	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, map[string]any{
		"token": token,
		"user": map[string]any{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
		},
	})
}
