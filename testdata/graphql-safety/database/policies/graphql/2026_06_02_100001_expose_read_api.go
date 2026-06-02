package graphql

type ExposeReadAPI_2026_06_02_100001 struct {
	GraphQLPolicy
}

func (p *ExposeReadAPI_2026_06_02_100001) Up() {
	p.Expose("users", func(e *ExposeBuilder) {
		e.List()
		e.Show()
		e.Relationship("posts", func(r *RelationshipExposure) {
			r.Cost(10)
			r.MaxPageSize(50)
		})
	})
	p.Expose("posts", func(e *ExposeBuilder) {
		e.List()
		e.Show()
		e.Relationship("comments", func(r *RelationshipExposure) {
			r.Cost(10)
			r.MaxPageSize(50)
		})
	})
	p.Expose("comments", func(e *ExposeBuilder) {
		e.List()
		e.Show()
	})
}

func (p *ExposeReadAPI_2026_06_02_100001) Down() {
	p.Unexpose("users")
	p.Unexpose("posts")
	p.Unexpose("comments")
}
