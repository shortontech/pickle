package requests

type UpdateUserRequest struct {
	Name  *string `json:"name" validate:"omitempty,min=1,max=255"`
	Email *string `json:"email" validate:"omitempty,email,max=255"`
}
