package graphql

type ExposeUsers_2026_03_25_100000 struct {
	GraphQLPolicy
}

func (m *ExposeUsers_2026_03_25_100000) Up() {
	m.Expose("users", func(e *ExposeBuilder) {
		e.List()
		e.Show()
	})
}

func (m *ExposeUsers_2026_03_25_100000) Down() {
	m.Unexpose("users")
}
