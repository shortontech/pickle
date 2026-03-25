package schema

// Policy is the base type embedded by all role policy structs.
// It records role operations for later execution or inspection.
type Policy struct {
	Operations []RoleOperation
}

// RoleOperation records a single role lifecycle change.
type RoleOperation struct {
	Type string // "create", "alter", "drop"
	Role RoleDefinition
}

// RoleDefinition describes a role and its permissions.
type RoleDefinition struct {
	Slug        string
	DisplayName string
	IsManages   bool
	IsDefault   bool
	Actions     []string

	// Used by alter operations to track removals.
	RemoveManages bool
	RemoveDefault bool
	RevokeActions []string
}

// RoleBuilder provides a fluent API for defining role operations.
type RoleBuilder struct {
	policy *Policy
	def    *RoleDefinition
}

// CreateRole begins a role creation operation.
func (p *Policy) CreateRole(slug string) *RoleBuilder {
	if slug == "" {
		panic("pickle: role slug must not be empty")
	}
	p.Operations = append(p.Operations, RoleOperation{
		Type: "create",
		Role: RoleDefinition{Slug: slug},
	})
	return &RoleBuilder{
		policy: p,
		def:    &p.Operations[len(p.Operations)-1].Role,
	}
}

// AlterRole begins a role alteration operation.
func (p *Policy) AlterRole(slug string) *RoleBuilder {
	if slug == "" {
		panic("pickle: role slug must not be empty")
	}
	p.Operations = append(p.Operations, RoleOperation{
		Type: "alter",
		Role: RoleDefinition{Slug: slug},
	})
	return &RoleBuilder{
		policy: p,
		def:    &p.Operations[len(p.Operations)-1].Role,
	}
}

// DropRole records a role drop operation.
func (p *Policy) DropRole(slug string) {
	if slug == "" {
		panic("pickle: role slug must not be empty")
	}
	p.Operations = append(p.Operations, RoleOperation{
		Type: "drop",
		Role: RoleDefinition{Slug: slug},
	})
}

// Name sets the display name for the role.
func (b *RoleBuilder) Name(name string) *RoleBuilder {
	b.def.DisplayName = name
	return b
}

// Manages marks this role as a managing role.
func (b *RoleBuilder) Manages() *RoleBuilder {
	b.def.IsManages = true
	return b
}

// RemoveManages removes the manages flag from this role.
func (b *RoleBuilder) RemoveManages() *RoleBuilder {
	b.def.RemoveManages = true
	return b
}

// Default marks this role as the default role for new users.
func (b *RoleBuilder) Default() *RoleBuilder {
	b.def.IsDefault = true
	return b
}

// RemoveDefault removes the default flag from this role.
func (b *RoleBuilder) RemoveDefault() *RoleBuilder {
	b.def.RemoveDefault = true
	return b
}

// Can grants the specified actions to this role.
func (b *RoleBuilder) Can(actions ...string) *RoleBuilder {
	b.def.Actions = append(b.def.Actions, actions...)
	return b
}

// RevokeCan removes the specified actions from this role (alter only).
func (b *RoleBuilder) RevokeCan(actions ...string) *RoleBuilder {
	b.def.RevokeActions = append(b.def.RevokeActions, actions...)
	return b
}

// GetOperations returns the operations recorded by Up() or Down().
func (p *Policy) GetOperations() []RoleOperation {
	return p.Operations
}

// Reset clears recorded operations so the policy struct can be reused.
func (p *Policy) Reset() {
	p.Operations = nil
}

// Transactional returns true — policies run in a transaction by default.
func (p *Policy) Transactional() bool {
	return true
}
