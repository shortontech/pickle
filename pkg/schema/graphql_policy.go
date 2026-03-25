package schema

// GraphQLPolicy is the base type embedded by all GraphQL exposure policy structs.
// It records GraphQL exposure operations for later execution or inspection.
type GraphQLPolicy struct {
	Operations []GraphQLOperation
}

// GraphQLOperation records a single GraphQL exposure change.
type GraphQLOperation struct {
	Type   string // "expose", "alter_expose", "unexpose", "controller_action", "remove_action"
	Model  string
	Ops    []ExposedOperation
	Action *ControllerActionDef
}

// ExposedOperation describes a single CRUD operation exposed over GraphQL.
type ExposedOperation struct {
	Type string // "list", "show", "create", "update", "delete"
}

// ControllerActionDef describes a custom controller action exposed as a GraphQL mutation.
type ControllerActionDef struct {
	Name    string
	Handler interface{}
}

// ExposeBuilder provides a fluent API for defining which operations to expose.
type ExposeBuilder struct {
	ops []ExposedOperation
}

// Expose registers a model with the specified operations for GraphQL exposure.
func (p *GraphQLPolicy) Expose(model string, fn func(*ExposeBuilder)) {
	if model == "" {
		panic("pickle: model name must not be empty")
	}
	b := &ExposeBuilder{}
	fn(b)
	p.Operations = append(p.Operations, GraphQLOperation{
		Type:  "expose",
		Model: model,
		Ops:   b.ops,
	})
}

// AlterExpose modifies the exposed operations for an already-exposed model.
func (p *GraphQLPolicy) AlterExpose(model string, fn func(*ExposeBuilder)) {
	if model == "" {
		panic("pickle: model name must not be empty")
	}
	b := &ExposeBuilder{}
	fn(b)
	p.Operations = append(p.Operations, GraphQLOperation{
		Type:  "alter_expose",
		Model: model,
		Ops:   b.ops,
	})
}

// Unexpose removes a model entirely from the GraphQL schema.
func (p *GraphQLPolicy) Unexpose(model string) {
	if model == "" {
		panic("pickle: model name must not be empty")
	}
	p.Operations = append(p.Operations, GraphQLOperation{
		Type:  "unexpose",
		Model: model,
	})
}

// ControllerAction registers a custom controller action as a GraphQL mutation.
func (p *GraphQLPolicy) ControllerAction(name string, handler interface{}) {
	if name == "" {
		panic("pickle: action name must not be empty")
	}
	p.Operations = append(p.Operations, GraphQLOperation{
		Type: "controller_action",
		Action: &ControllerActionDef{
			Name:    name,
			Handler: handler,
		},
	})
}

// RemoveAction removes a previously registered controller action.
func (p *GraphQLPolicy) RemoveAction(name string) {
	if name == "" {
		panic("pickle: action name must not be empty")
	}
	p.Operations = append(p.Operations, GraphQLOperation{
		Type: "remove_action",
		Action: &ControllerActionDef{
			Name: name,
		},
	})
}

// List adds the list operation.
func (e *ExposeBuilder) List() { e.ops = append(e.ops, ExposedOperation{Type: "list"}) }

// Show adds the show operation.
func (e *ExposeBuilder) Show() { e.ops = append(e.ops, ExposedOperation{Type: "show"}) }

// Create adds the create operation.
func (e *ExposeBuilder) Create() { e.ops = append(e.ops, ExposedOperation{Type: "create"}) }

// Update adds the update operation.
func (e *ExposeBuilder) Update() { e.ops = append(e.ops, ExposedOperation{Type: "update"}) }

// Delete adds the delete operation.
func (e *ExposeBuilder) Delete() { e.ops = append(e.ops, ExposedOperation{Type: "delete"}) }

// All adds all five CRUD operations (list, show, create, update, delete).
func (e *ExposeBuilder) All() {
	e.List()
	e.Show()
	e.Create()
	e.Update()
	e.Delete()
}

// RemoveList marks the list operation for removal (alter only).
func (e *ExposeBuilder) RemoveList() {
	e.ops = append(e.ops, ExposedOperation{Type: "remove_list"})
}

// RemoveShow marks the show operation for removal (alter only).
func (e *ExposeBuilder) RemoveShow() {
	e.ops = append(e.ops, ExposedOperation{Type: "remove_show"})
}

// RemoveCreate marks the create operation for removal (alter only).
func (e *ExposeBuilder) RemoveCreate() {
	e.ops = append(e.ops, ExposedOperation{Type: "remove_create"})
}

// RemoveUpdate marks the update operation for removal (alter only).
func (e *ExposeBuilder) RemoveUpdate() {
	e.ops = append(e.ops, ExposedOperation{Type: "remove_update"})
}

// RemoveDelete marks the delete operation for removal (alter only).
func (e *ExposeBuilder) RemoveDelete() {
	e.ops = append(e.ops, ExposedOperation{Type: "remove_delete"})
}

// GetOperations returns the operations recorded by Up() or Down().
func (p *GraphQLPolicy) GetOperations() []GraphQLOperation {
	return p.Operations
}

// Reset clears recorded operations so the policy struct can be reused.
func (p *GraphQLPolicy) Reset() {
	p.Operations = nil
}

// Transactional returns true — policies run in a transaction by default.
func (p *GraphQLPolicy) Transactional() bool {
	return true
}
