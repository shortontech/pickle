package controllers

import (
	pickle "monorepo/services/worker/http"
	"monorepo/app/models"

	"github.com/google/uuid"
)

type JobController struct {
	pickle.Controller
}

func (c JobController) Index(ctx *pickle.Context) pickle.Response {
	jobs, err := models.QueryJob().
		WhereUserID(uuid.MustParse(ctx.Auth().UserID)).
		All()
	if err != nil {
		return ctx.Error(err)
	}
	return ctx.JSON(200, jobs)
}

func (c JobController) Show(ctx *pickle.Context) pickle.Response {
	job, err := models.QueryJob().
		WhereID(uuid.MustParse(ctx.Param("id"))).
		First()
	if err != nil {
		return ctx.NotFound("job not found")
	}
	return ctx.JSON(200, job)
}
