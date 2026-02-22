package basiccrud

var API = pickle.Routes(func(r *pickle.Router) {
	r.Group("/api", func(r *pickle.Router) {
		r.Resource("/users", UserController{})

		r.Group("/posts", Auth, func(r *pickle.Router) {
			r.Get("/", PostController{}.Index)
			r.Get("/:id", PostController{}.Show)
			r.Post("/", PostController{}.Store)
			r.Put("/:id", PostController{}.Update)
			r.Delete("/:id", PostController{}.Destroy)
		})
	})
})
