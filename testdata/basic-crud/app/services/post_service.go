package services

import (
	"github.com/google/uuid"
	"github.com/shortontech/pickle/testdata/basic-crud/app/models"
)

func CreatePost(userID uuid.UUID, title, body string) (*models.Post, error) {
	post := &models.Post{
		UserID: userID,
		Title:  title,
		Body:   body,
		Status: "draft",
	}

	if err := models.QueryPost().Create(post); err != nil {
		return nil, err
	}

	return post, nil
}
