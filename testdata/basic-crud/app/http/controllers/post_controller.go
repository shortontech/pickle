package controllers

import (
	pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"
	"github.com/shortontech/pickle/testdata/basic-crud/app/http/requests"
	"github.com/shortontech/pickle/testdata/basic-crud/app/models"

	"github.com/google/uuid"
)

type PostController struct {
	pickle.Controller
}

func (c PostController) Index(ctx *pickle.Context) pickle.Response {
	posts, err := models.QueryPost().
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		WithUser().
		All()

	if err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, posts)
}

func (c PostController) Show(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid id"})
	}

	post, err := models.QueryPost().
		WhereID(id).
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		WithUser().
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	return ctx.JSON(200, post)
}

func (c PostController) Store(ctx *pickle.Context) pickle.Response {
	req, bindErr := requests.BindCreatePostRequest(ctx.Request())
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

func (c PostController) Update(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid id"})
	}

	req, bindErr := requests.BindUpdatePostRequest(ctx.Request())
	if bindErr != nil {
		return ctx.JSON(bindErr.Status, bindErr)
	}

	post, err := models.QueryPost().
		WhereID(id).
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		First()

	if err != nil {
		return ctx.NotFound("post not found")
	}

	if req.Title != nil {
		post.Title = *req.Title
	}
	if req.Body != nil {
		post.Body = *req.Body
	}
	if req.Status != nil {
		post.Status = *req.Status
	}

	if err := models.QueryPost().Update(post); err != nil {
		return ctx.Error(err)
	}

	return ctx.JSON(200, post)
}

func (c PostController) Destroy(ctx *pickle.Context) pickle.Response {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return ctx.JSON(400, map[string]string{"error": "invalid id"})
	}

	post, err := models.QueryPost().
		WhereID(id).
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
