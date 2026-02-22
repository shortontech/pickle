package cooked

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseWrite(t *testing.T) {
	w := httptest.NewRecorder()
	r := Response{
		StatusCode: 201,
		Body:       map[string]string{"id": "1"},
		Headers:    map[string]string{"X-Custom": "val"},
	}
	r.Write(w)

	if w.Code != 201 {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"id":"1"`) {
		t.Errorf("body = %s, want to contain id:1", w.Body.String())
	}
	if w.Header().Get("X-Custom") != "val" {
		t.Errorf("X-Custom header = %q, want val", w.Header().Get("X-Custom"))
	}
}

func TestResponseWriteNoBody(t *testing.T) {
	w := httptest.NewRecorder()
	Response{StatusCode: 204}.Write(w)
	if w.Code != 204 {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestResponseHeader(t *testing.T) {
	r := Response{StatusCode: 200}
	r2 := r.Header("X-Foo", "bar")
	if r2.Headers["X-Foo"] != "bar" {
		t.Errorf("Header not set")
	}
}
