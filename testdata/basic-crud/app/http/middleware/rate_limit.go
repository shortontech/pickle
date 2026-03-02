package middleware

import pickle "github.com/shortontech/pickle/testdata/basic-crud/app/http"

// RateLimit is a stub rate limiting middleware.
// In production, this would use a token bucket or sliding window algorithm.
func RateLimit(ctx *pickle.Context, next func() pickle.Response) pickle.Response {
	return next()
}
