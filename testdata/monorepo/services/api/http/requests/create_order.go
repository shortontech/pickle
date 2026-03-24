package requests

type CreateOrderRequest struct {
	Total    string `json:"total" validate:"required"`
	Currency string `json:"currency" validate:"required,oneof=USD EUR GBP"`
}
