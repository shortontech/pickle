package controllers

import (
	"strings"

	pickle "github.com/shortontech/pickle/testdata/adminlte-session/app/http"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/auth/session"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/http/requests"
	"github.com/shortontech/pickle/testdata/adminlte-session/app/models"
	"golang.org/x/crypto/bcrypt"
)

type SessionController struct{ pickle.Controller }

const dummyPasswordHash = "$2a$10$gRGHNQkSWYpZMo2NPseTqOQMvKx4mXFJro7qvAXn6vqYvu34XssVK"

func (SessionController) Create(ctx *pickle.Context) pickle.Response {
	data := pickle.LoginData{}
	data.Page.Title = "Sign in"
	data.Page.Heading = "AdminLTE session demo"
	data.Email = "admin@example.test"
	data.CsrfToken, _ = ctx.Cookie("csrf_token")
	return pickle.Login(ctx, data)
}

func (SessionController) Store(ctx *pickle.Context) pickle.Response {
	req, bindErr := requests.BindSessionForm(ctx.Request())
	if bindErr != nil {
		return invalidLogin(ctx, req.Email)
	}

	user, err := models.QueryUser().WhereEmail(strings.ToLower(strings.TrimSpace(req.Email))).First()
	passwordHash := dummyPasswordHash
	if user != nil {
		passwordHash = user.PasswordHash
	}
	passwordMatches := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) == nil
	if err != nil || !passwordMatches {
		return invalidLogin(ctx, req.Email)
	}

	cookies, err := session.Create(ctx, user.ID.String(), user.Role)
	if err != nil {
		return ctx.Error(err)
	}
	return cookies.Apply(ctx.Redirect("/"))
}

func invalidLogin(ctx *pickle.Context, email string) pickle.Response {
	data := pickle.LoginData{}
	data.Page.Title = "Sign in"
	data.Page.Heading = "AdminLTE session demo"
	data.Email = email
	data.HasError = true
	data.Error = "The provided credentials do not match our records."
	data.CsrfToken, _ = ctx.Cookie("csrf_token")
	response := pickle.Login(ctx, data)
	response.StatusCode = 422
	return response
}

func (SessionController) Destroy(ctx *pickle.Context) pickle.Response {
	response, err := session.Destroy(ctx)
	if err != nil {
		return ctx.Error(err)
	}
	response.StatusCode = 303
	return response.Header("Location", "/login").Header("Cache-Control", "no-store")
}
