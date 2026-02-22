package cooked

// RunMiddleware executes a middleware stack around a handler.
// Middleware functions are called in order, each wrapping the next.
func RunMiddleware(ctx *Context, middleware []MiddlewareFunc, handler func() Response) Response {
	if len(middleware) == 0 {
		return handler()
	}

	// Build the chain from the inside out.
	next := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		mw := middleware[i]
		inner := next
		next = func() Response {
			return mw(ctx, inner)
		}
	}

	return next()
}
