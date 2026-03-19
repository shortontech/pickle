package requests

type CreateAccountRequest struct {
	Name     string `json:"name" validate:"required,min=1,max=100"`
	Currency string `json:"currency" validate:"required,len=3"`
	Type     string `json:"type" validate:"required,oneof=checking savings credit"`
}
