package requests

type UpdateAccountRequest struct {
	Name   *string `json:"name" validate:"omitempty,min=1,max=100"`
	Active *bool   `json:"active" validate:"omitempty"`
}
