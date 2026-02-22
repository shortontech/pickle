package requests

type CreatePostRequest struct {
	Title string `json:"title" validate:"required,min=1,max=255"`
	Body  string `json:"body" validate:"required,min=1"`
}
