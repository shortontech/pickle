package cooked

import "testing"

func TestRouterAcceptsDeclaredMiddlewareFunction(t *testing.T) {
	declared := func(_ *Context, next func() Response) Response { return next() }
	router := Routes(func(router *Router) {
		router.Get("/", func(*Context) Response { return Response{StatusCode: 204} }, declared)
	})
	if got := len(router.routes[0].Middleware); got != 1 {
		t.Fatalf("middleware count = %d", got)
	}
}
