package requests

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBindSessionForm(t *testing.T) {
	form := url.Values{"email": {"admin@example.test"}, "password": {"password"}}
	request := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	got, bindErr := BindSessionForm(request)
	if bindErr != nil {
		t.Fatalf("BindSessionForm() error = %v", bindErr)
	}
	if got.Email != "admin@example.test" || got.Password != "password" {
		t.Fatalf("BindSessionForm() = %#v", got)
	}
}

func TestBindSessionFormRejectsInvalidCredentialsShape(t *testing.T) {
	form := url.Values{"email": {"not-an-email"}, "password": {"short"}}
	request := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, bindErr := BindSessionForm(request)
	if bindErr == nil || bindErr.Status != 422 {
		t.Fatalf("BindSessionForm() error = %#v, want status 422", bindErr)
	}
}
