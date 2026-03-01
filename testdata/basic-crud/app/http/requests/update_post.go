package requests

type UpdatePostRequest struct {
	Title  *string `json:"title" validate:"omitempty,min=1,max=255"`
	Body   *string `json:"body" validate:"omitempty,min=1"`
	Status *string `json:"status" validate:"omitempty,oneof=draft published archived"`
}
