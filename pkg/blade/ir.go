// Package blade compiles Pickle's safe Blade-shaped view language.
package blade

// Span identifies a byte range in an authored template.
type Span struct {
	Start int
	End   int
}

// Document is target-neutral view IR. It contains presentation intent, not Go
// or PHP renderer fragments.
type Document struct {
	Name         string
	Source       string
	Nodes        []Node
	Dependencies []Dependency
}

type Dependency struct {
	Kind string
	Name string
	Span Span
}

type Node interface {
	node()
	SourceSpan() Span
}

type Text struct {
	Value string
	Span  Span
}

func (Text) node()              {}
func (n Text) SourceSpan() Span { return n.Span }

type Escaped struct {
	Path []string
	Span Span
}

func (Escaped) node()              {}
func (n Escaped) SourceSpan() Span { return n.Span }

// Asset is a statically named asset dependency. The authored name is resolved
// to a content-addressed URL before Go emission.
type Asset struct {
	Name string
	Span Span
}

func (Asset) node()              {}
func (n Asset) SourceSpan() Span { return n.Span }

type RouteURL struct {
	Name string
	Span Span
}

func (RouteURL) node()              {}
func (n RouteURL) SourceSpan() Span { return n.Span }

type CSRF struct{ Span Span }

func (CSRF) node()              {}
func (n CSRF) SourceSpan() Span { return n.Span }

type RouteIs struct {
	Pattern string
	Then    []Node
	Else    []Node
	Span    Span
}

func (RouteIs) node()              {}
func (n RouteIs) SourceSpan() Span { return n.Span }

type If struct {
	Condition []string
	Then      []Node
	Else      []Node
	Span      Span
}

func (If) node()              {}
func (n If) SourceSpan() Span { return n.Span }

type ForEach struct {
	Collection []string
	Item       string
	Body       []Node
	Span       Span
}

func (ForEach) node()              {}
func (n ForEach) SourceSpan() Span { return n.Span }

type Extends struct {
	Name string
	Span Span
}

func (Extends) node()              {}
func (n Extends) SourceSpan() Span { return n.Span }

type Section struct {
	Name string
	Body []Node
	Span Span
}

func (Section) node()              {}
func (n Section) SourceSpan() Span { return n.Span }

type Yield struct {
	Name string
	Span Span
}

func (Yield) node()              {}
func (n Yield) SourceSpan() Span { return n.Span }

type Include struct {
	Name string
	Span Span
}

func (Include) node()              {}
func (n Include) SourceSpan() Span { return n.Span }
