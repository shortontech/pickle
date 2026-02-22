package controllers

type UserController struct {
	Controller
}

func (c *UserController) Index(ctx *Context) Response {
	users, err := Query[User]().All()
	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, users)
}

func (c *UserController) Show(ctx *Context) Response {
	user, err := Query[User]().
		WhereID(ctx.Param("id")).
		First()

	if err != nil {
		return ctx.NotFound("user not found")
	}

	return ctx.JSON(200, user)
}

func (c *UserController) Store(req CreateUserRequest, ctx *Context) Response {
	user := &User{
		Name:     req.Name,
		Email:    req.Email,
		Password: HashPassword(req.Password),
	}

	if err := Query[User]().Create(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, user)
}

func (c *UserController) Update(req UpdateUserRequest, ctx *Context) Response {
	user, err := Query[User]().
		WhereID(ctx.Param("id")).
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

	if err := Query[User]().Update(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, user)
}

func (c *UserController) Destroy(ctx *Context) Response {
	user, err := Query[User]().
		WhereID(ctx.Param("id")).
		First()

	if err != nil {
		return ctx.NotFound("user not found")
	}

	if err := Query[User]().Delete(user); err != nil {
		return ctx.Error(err)
	}

	return ctx.NoContent()
}
