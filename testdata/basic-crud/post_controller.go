package basiccrud

import "github.com/pickle-framework/pickle/testdata/basic-crud/models"

type PostController struct {
	Controller
}

func (c *PostController) Index(ctx *Context) Response {
	posts, err := models.Query[models.Post]().
		WhereUserID(ctx.Auth().UserID).
		WithUser().
		All()

	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, posts)
}

func (c *PostController) Show(ctx *Context) Response {
	post, err := models.Query[models.Post]().
		WhereID(ctx.Param("id")).
		WhereUserID(ctx.Auth().UserID).
		WithUser().
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	return ctx.JSON(200, post)
}

func (c *PostController) Store(req CreatePostRequest, ctx *Context) Response {
	post := &models.Post{
		UserID: ctx.Auth().UserID,
		Title:  req.Title,
		Body:   req.Body,
		Status: "draft",
	}

	if err := models.Query[models.Post]().Create(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, post)
}

func (c *PostController) Update(req UpdatePostRequest, ctx *Context) Response {
	post, err := models.Query[models.Post]().
		WhereID(ctx.Param("id")).
		WhereUserID(ctx.Auth().UserID).
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	if req.Title != "" {
		post.Title = req.Title
	}
	if req.Body != "" {
		post.Body = req.Body
	}
	if req.Status != "" {
		post.Status = req.Status
	}

	if err := models.Query[models.Post]().Update(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, post)
}

func (c *PostController) Destroy(ctx *Context) Response {
	post, err := models.Query[models.Post]().
		WhereID(ctx.Param("id")).
		WhereUserID(ctx.Auth().UserID).
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	if err := models.Query[models.Post]().Delete(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.NoContent()
}
