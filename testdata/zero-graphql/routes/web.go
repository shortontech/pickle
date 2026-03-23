package routes

import (
	pickle "github.com/shortontech/pickle/testdata/zero-graphql/app/http"
)

// API defines the routes. For zero-controller GraphQL, routes are minimal —
// the GraphQL handler is wired up separately.
var API = pickle.Routes(func(r *pickle.Router) {
	// No REST controllers needed — everything is served via GraphQL.
})
