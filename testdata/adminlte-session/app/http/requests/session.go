package requests

import "net/http"

type SessionRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// BindSessionForm binds and validates an HTML form using the same validation
// contract as generated JSON request bindings.
func BindSessionForm(r *http.Request) (SessionRequest, *BindingError) {
	var req SessionRequest
	if err := r.ParseForm(); err != nil {
		return req, &BindingError{Status: http.StatusBadRequest, Errors: []ValidationError{{Field: "_body", Message: "invalid form body"}}}
	}
	req.Email = r.Form.Get("email")
	req.Password = r.Form.Get("password")
	if err := validate.Struct(req); err != nil {
		return req, formatValidationErrors(err)
	}
	return req, nil
}
