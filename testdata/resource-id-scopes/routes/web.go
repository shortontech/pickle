package routes

var API = pickle.Routes(func(r *pickle.Router) {
	r.Get("/records/:record_id", controllers.RecordController{}.Show, middleware.Auth)
	r.Get("/unsafe/records/:record_id", controllers.RecordController{}.UnsafeShow, middleware.Auth)
})
