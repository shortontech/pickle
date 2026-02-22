package cooked

import (
	"net/http/httptest"
	"testing"
)

func TestRunMiddlewareEmpty(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	resp := RunMiddleware(ctx, nil, func() Response {
		return Response{StatusCode: 200, Body: "ok"}
	})
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRunMiddlewareChain(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	var order []string

	mw1 := MiddlewareFunc(func(ctx *Context, next func() Response) Response {
		order = append(order, "mw1-before")
		resp := next()
		order = append(order, "mw1-after")
		return resp
	})
	mw2 := MiddlewareFunc(func(ctx *Context, next func() Response) Response {
		order = append(order, "mw2-before")
		resp := next()
		order = append(order, "mw2-after")
		return resp
	})

	resp := RunMiddleware(ctx, []MiddlewareFunc{mw1, mw2}, func() Response {
		order = append(order, "handler")
		return Response{StatusCode: 200}
	})

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("execution order = %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

func TestRunMiddlewareShortCircuit(t *testing.T) {
	ctx := NewContext(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))

	authMW := MiddlewareFunc(func(ctx *Context, next func() Response) Response {
		return Response{StatusCode: 401, Body: map[string]string{"error": "unauthorized"}}
	})

	handlerCalled := false
	resp := RunMiddleware(ctx, []MiddlewareFunc{authMW}, func() Response {
		handlerCalled = true
		return Response{StatusCode: 200}
	})

	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if handlerCalled {
		t.Error("handler should not have been called")
	}
}
