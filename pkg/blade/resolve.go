package blade

import (
	"fmt"
	"sort"
	"strings"
)

// Resolve composes layouts, sections, yields, and includes. Only entry views
// (documents not referenced by another view) are returned as renderers.
func Resolve(documents []*Document) ([]*Document, error) {
	byName := make(map[string]*Document, len(documents))
	referenced := map[string]bool{}
	for _, document := range documents {
		name := normalizeViewName(document.Name)
		document.Name = name
		if byName[name] != nil {
			return nil, fmt.Errorf("duplicate view %q", name)
		}
		byName[name] = document
		for _, dependency := range document.Dependencies {
			if dependency.Kind == "layout" || dependency.Kind == "include" {
				referenced[dependency.Name] = true
			}
		}
	}
	var roots []string
	for name := range byName {
		if !referenced[name] {
			roots = append(roots, name)
		}
	}
	if len(roots) == 0 && len(byName) > 0 {
		for name := range byName {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	var resolved []*Document
	for _, name := range roots {
		nodes, err := resolveView(name, nil, byName, nil)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, &Document{Name: name, Nodes: nodes})
	}
	return resolved, nil
}

func resolveView(name string, incoming map[string][]Node, documents map[string]*Document, stack []string) ([]Node, error) {
	for _, active := range stack {
		if active == name {
			return nil, fmt.Errorf("view dependency cycle: %s", strings.Join(append(stack, name), " -> "))
		}
	}
	document := documents[name]
	if document == nil {
		return nil, fmt.Errorf("view dependency %q does not exist", name)
	}
	stack = append(stack, name)
	sections := map[string][]Node{}
	var layout *Extends
	var body []Node
	for _, node := range document.Nodes {
		switch node := node.(type) {
		case Extends:
			if layout != nil {
				return nil, viewDiagnostic(document, node.Span, "multiple @extends directives")
			}
			copy := node
			layout = &copy
		case Section:
			if _, duplicate := sections[node.Name]; duplicate {
				return nil, viewDiagnostic(document, node.Span, "duplicate section %q", node.Name)
			}
			sections[node.Name] = node.Body
		default:
			body = append(body, node)
		}
	}
	for section, nodes := range incoming {
		sections[section] = nodes
	}
	if layout != nil {
		for _, node := range body {
			if text, ok := node.(Text); !ok || strings.TrimSpace(text.Value) != "" {
				return nil, viewDiagnostic(document, node.SourceSpan(), "content outside @section in an extending view")
			}
		}
		if documents[layout.Name] == nil {
			return nil, viewDiagnostic(document, layout.Span, "view dependency %q does not exist", layout.Name)
		}
		for _, active := range stack {
			if active == layout.Name {
				return nil, viewDiagnostic(document, layout.Span, "view dependency cycle: %s", strings.Join(append(stack, layout.Name), " -> "))
			}
		}
		return resolveView(layout.Name, sections, documents, stack)
	}
	if len(sections) > 0 && incoming == nil {
		return nil, viewDiagnostic(document, Span{}, "@section requires @extends")
	}
	return expandNodes(body, sections, documents, stack)
}

func expandNodes(nodes []Node, sections map[string][]Node, documents map[string]*Document, stack []string) ([]Node, error) {
	var out []Node
	for _, node := range nodes {
		switch node := node.(type) {
		case Yield:
			body, ok := sections[node.Name]
			if !ok {
				return nil, viewDiagnostic(documents[stack[len(stack)-1]], node.Span, "missing section %q", node.Name)
			}
			expanded, err := expandNodes(body, sections, documents, stack)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
		case Include:
			current := documents[stack[len(stack)-1]]
			if documents[node.Name] == nil {
				return nil, viewDiagnostic(current, node.Span, "view dependency %q does not exist", node.Name)
			}
			for _, active := range stack {
				if active == node.Name {
					return nil, viewDiagnostic(current, node.Span, "view dependency cycle: %s", strings.Join(append(stack, node.Name), " -> "))
				}
			}
			expanded, err := resolveView(node.Name, nil, documents, stack)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
		case If:
			thenNodes, err := expandNodes(node.Then, sections, documents, stack)
			if err != nil {
				return nil, err
			}
			elseNodes, err := expandNodes(node.Else, sections, documents, stack)
			if err != nil {
				return nil, err
			}
			node.Then, node.Else = thenNodes, elseNodes
			out = append(out, node)
		case ForEach:
			body, err := expandNodes(node.Body, sections, documents, stack)
			if err != nil {
				return nil, err
			}
			node.Body = body
			out = append(out, node)
		case Extends, Section:
			return nil, fmt.Errorf("%s: invalid nested view directive", stack[len(stack)-1])
		default:
			out = append(out, node)
		}
	}
	return out, nil
}

func viewDiagnostic(document *Document, span Span, format string, args ...any) error {
	if document != nil && document.Source != "" {
		return diagnostic(document.Name, document.Source, span.Start, format, args...)
	}
	name := "view"
	if document != nil {
		name = document.Name
	}
	return fmt.Errorf("%s: %s", name, fmt.Sprintf(format, args...))
}
