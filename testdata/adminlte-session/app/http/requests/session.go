package requests

// SessionRequest documents the eventual credential form contract. The current
// fixture's deterministic login does not bind it yet.
type SessionRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}
