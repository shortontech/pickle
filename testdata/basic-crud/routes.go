package basiccrud

var API = Routes(func(r *Router) {
	r.Group("/api", func(r *Router) {
		r.Resource("/users", UserController{})

		r.Group("/posts", Auth, func(r *Router) {
			r.Get("/", PostController{}.Index)
			r.Get("/:id", PostController{}.Show)
			r.Post("/", PostController{}.Store)
			r.Put("/:id", PostController{}.Update)
			r.Delete("/:id", PostController{}.Destroy)
		})
	})
})
