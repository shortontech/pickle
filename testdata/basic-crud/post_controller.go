package basiccrud

import (
	"github.com/google/uuid"
	"github.com/pickle-framework/pickle/testdata/basic-crud/models"
)

type PostController struct {
	Controller
}

func (c PostController) Index(ctx *Context) Response {
	posts, err := models.QueryPost().
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		WithUser().
		All()

	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, posts)
}

func (c PostController) Show(ctx *Context) Response {
	post, err := models.QueryPost().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		WithUser().
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	return ctx.JSON(200, post)
}

func (c PostController) Store(ctx *Context) Response {
	req, bindErr := BindCreatePostRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	post := &models.Post{
		UserID: uuid.MustParse(ctx.Auth().UserID),
		Title:  req.Title,
		Body:   req.Body,
		Status: "draft",
	}

	if err := models.QueryPost().Create(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(201, post)
}

func (c PostController) Update(ctx *Context) Response {
	req, bindErr := BindUpdatePostRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	post, err := models.QueryPost().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
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

	if err := models.QueryPost().Update(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, post)
}

func (c PostController) Destroy(ctx *Context) Response {
	post, err := models.QueryPost().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	if err := models.QueryPost().Delete(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.NoContent()
}
