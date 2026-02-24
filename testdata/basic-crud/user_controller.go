package basiccrud

import (
	"github.com/google/uuid"
	"github.com/pickle-framework/pickle/testdata/basic-crud/models"
)

type UserController struct {
	Controller
}

func (c UserController) Index(ctx *Context) Response {
	users, err := models.QueryUser().All()
	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, users)
}

func (c UserController) Show(ctx *Context) Response {
	user, err := models.QueryUser().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		First()

	if err != nil {
		return ctx.NotFound("user not found")
	}

	return ctx.JSON(200, user)
}

func (c UserController) Store(req CreateUserRequest, ctx *Context) Response {
	user := &models.User{
		Name:     req.Name,
		Email:    req.Email,
		Password: HashPassword(req.Password),
	}

	if err := models.QueryUser().Create(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, user)
}

func (c UserController) Update(req UpdateUserRequest, ctx *Context) Response {
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

func (c UserController) Destroy(ctx *Context) Response {
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
