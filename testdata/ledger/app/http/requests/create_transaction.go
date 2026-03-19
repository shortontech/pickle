package requests

type CreateTransactionRequest struct {
	Type        string  `json:"type" validate:"required,oneof=debit credit fee"`
	Amount      string  `json:"amount" validate:"required,numeric"`
	Currency    string  `json:"currency" validate:"required,len=3"`
	Description *string `json:"description" validate:"omitempty,max=255"`
}
