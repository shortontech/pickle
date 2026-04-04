package policies

type CreateInitialRoles_2026_03_23_100000 struct {
	Policy
}

func (m *CreateInitialRoles_2026_03_23_100000) Up() {
	m.CreateRole("admin").Name("Administrator").Manages().Can("users.create", "users.delete")
	m.CreateRole("editor").Name("Editor").Can("posts.create", "posts.edit")
	m.CreateRole("viewer").Name("Viewer").Default()
}

func (m *CreateInitialRoles_2026_03_23_100000) Down() {
	m.DropRole("viewer")
	m.DropRole("editor")
	m.DropRole("admin")
}
