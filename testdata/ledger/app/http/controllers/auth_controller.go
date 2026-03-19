package controllers

import (
	pickle "github.com/shortontech/ledger/app/http"
	"github.com/shortontech/ledger/app/http/auth"
	"github.com/shortontech/ledger/app/http/auth/jwt"
	"github.com/shortontech/ledger/app/http/requests"
	"github.com/shortontech/ledger/app/models"
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

	if !CheckPassword(user.Password, req.Password) {
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
		"user":  user.Public(),
	})
}
