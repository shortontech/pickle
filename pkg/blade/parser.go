package blade

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	directiveRE = regexp.MustCompile(`^@(if|foreach)\s*\(([^)\n]*)\)`)
	pathRE      = regexp.MustCompile(`^\$[A-Za-z_][A-Za-z0-9_]*(?:->[A-Za-z_][A-Za-z0-9_]*)*$`)
	foreachRE   = regexp.MustCompile(`^(\$[A-Za-z_][A-Za-z0-9_]*(?:->[A-Za-z_][A-Za-z0-9_]*)*)\s+as\s+(\$[A-Za-z_][A-Za-z0-9_]*)$`)
	assetRE     = regexp.MustCompile(`^asset\(\s*(['"])([^'"]+)['"]\s*\)$`)
	viewCallRE  = regexp.MustCompile(`^@(extends|include|section|yield)\s*\(\s*['"]([^'"]+)['"]\s*\)`)
)

// Parse converts an authored template to target-neutral IR. It deliberately
// rejects PHP rather than falling back to evaluating it.
func Parse(name, source string) (*Document, error) {
	for _, forbidden := range []string{"<?php", "<?=", "@php", "@endphp", "{!!"} {
		if at := strings.Index(source, forbidden); at >= 0 {
			return nil, diagnostic(name, source, at, "forbidden PHP or raw output %q", forbidden)
		}
	}
	p := parser{name: name, source: source}
	nodes, stop, err := p.nodes(nil)
	if err != nil {
		return nil, err
	}
	if stop != "" {
		return nil, diagnostic(name, source, p.pos, "unexpected @%s", stop)
	}
	doc := &Document{Name: name, Source: source, Nodes: nodes}
	collectDependencies(nodes, &doc.Dependencies)
	return doc, nil
}

type parser struct {
	name   string
	source string
	pos    int
}

func (p *parser) nodes(stops map[string]bool) ([]Node, string, error) {
	var out []Node
	for p.pos < len(p.source) {
		if strings.HasPrefix(p.source[p.pos:], "{{--") {
			end := strings.Index(p.source[p.pos+4:], "--}}")
			if end < 0 {
				return nil, "", diagnostic(p.name, p.source, p.pos, "unterminated Blade comment")
			}
			p.pos += 4 + end + 4
			continue
		}
		if strings.HasPrefix(p.source[p.pos:], "{{") {
			start := p.pos
			end := strings.Index(p.source[p.pos+2:], "}}")
			if end < 0 {
				return nil, "", diagnostic(p.name, p.source, start, "unterminated escaped expression")
			}
			raw := strings.TrimSpace(p.source[p.pos+2 : p.pos+2+end])
			p.pos += 2 + end + 2
			if match := assetRE.FindStringSubmatch(raw); match != nil {
				name := strings.TrimPrefix(match[2], "/")
				if name == "" || strings.Contains(name, "..") || strings.ContainsRune(name, '\\') {
					return nil, "", diagnostic(p.name, p.source, start, "invalid asset path %q", match[2])
				}
				out = append(out, Asset{Name: name, Span: Span{Start: start, End: p.pos}})
				continue
			}
			path, err := parsePath(raw)
			if err != nil {
				return nil, "", diagnostic(p.name, p.source, start, "%v", err)
			}
			out = append(out, Escaped{Path: path, Span: Span{Start: start, End: p.pos}})
			continue
		}
		if p.source[p.pos] == '@' {
			word := directiveWord(p.source[p.pos+1:])
			if stops != nil && stops[word] {
				p.pos += 1 + len(word)
				return out, word, nil
			}
			start := p.pos
			if call := viewCallRE.FindStringSubmatch(p.source[p.pos:]); call != nil {
				p.pos += len(call[0])
				name := call[2]
				switch call[1] {
				case "extends":
					out = append(out, Extends{Name: normalizeViewName(name), Span: Span{Start: start, End: p.pos}})
				case "include":
					out = append(out, Include{Name: normalizeViewName(name), Span: Span{Start: start, End: p.pos}})
				case "yield":
					out = append(out, Yield{Name: name, Span: Span{Start: start, End: p.pos}})
				case "section":
					body, stop, err := p.nodes(map[string]bool{"endsection": true})
					if err != nil {
						return nil, "", err
					}
					if stop != "endsection" {
						return nil, "", diagnostic(p.name, p.source, start, "unterminated @section")
					}
					out = append(out, Section{Name: name, Body: body, Span: Span{Start: start, End: p.pos}})
				}
				continue
			}
			m := directiveRE.FindStringSubmatch(p.source[p.pos:])
			if m == nil {
				return nil, "", diagnostic(p.name, p.source, p.pos, "unsupported directive @%s", word)
			}
			p.pos += len(m[0])
			switch m[1] {
			case "if":
				cond, err := parsePath(strings.TrimSpace(m[2]))
				if err != nil {
					return nil, "", diagnostic(p.name, p.source, start, "@if requires a boolean path: %v", err)
				}
				then, stop, err := p.nodes(map[string]bool{"else": true, "endif": true})
				if err != nil {
					return nil, "", err
				}
				var otherwise []Node
				if stop == "else" {
					otherwise, stop, err = p.nodes(map[string]bool{"endif": true})
					if err != nil {
						return nil, "", err
					}
				}
				if stop != "endif" {
					return nil, "", diagnostic(p.name, p.source, start, "unterminated @if")
				}
				out = append(out, If{Condition: cond, Then: then, Else: otherwise, Span: Span{Start: start, End: p.pos}})
			case "foreach":
				fm := foreachRE.FindStringSubmatch(strings.TrimSpace(m[2]))
				if fm == nil {
					return nil, "", diagnostic(p.name, p.source, start, "@foreach requires '$collection as $item'")
				}
				collection, _ := parsePath(fm[1])
				body, stop, err := p.nodes(map[string]bool{"endforeach": true})
				if err != nil {
					return nil, "", err
				}
				if stop != "endforeach" {
					return nil, "", diagnostic(p.name, p.source, start, "unterminated @foreach")
				}
				out = append(out, ForEach{Collection: collection, Item: strings.TrimPrefix(fm[2], "$"), Body: body, Span: Span{Start: start, End: p.pos}})
			}
			continue
		}
		start := p.pos
		nextExpr := strings.Index(p.source[p.pos:], "{{")
		nextDirective := strings.IndexByte(p.source[p.pos:], '@')
		next := smallestPositive(nextExpr, nextDirective)
		if next < 0 {
			p.pos = len(p.source)
		} else if next == 0 {
			p.pos++
			continue
		} else {
			p.pos += next
		}
		out = append(out, Text{Value: p.source[start:p.pos], Span: Span{Start: start, End: p.pos}})
	}
	return out, "", nil
}

func collectDependencies(nodes []Node, dependencies *[]Dependency) {
	for _, node := range nodes {
		switch node := node.(type) {
		case Asset:
			*dependencies = append(*dependencies, Dependency{Kind: "asset", Name: node.Name, Span: node.Span})
		case If:
			collectDependencies(node.Then, dependencies)
			collectDependencies(node.Else, dependencies)
		case ForEach:
			collectDependencies(node.Body, dependencies)
		case Extends:
			*dependencies = append(*dependencies, Dependency{Kind: "layout", Name: node.Name, Span: node.Span})
		case Include:
			*dependencies = append(*dependencies, Dependency{Kind: "include", Name: node.Name, Span: node.Span})
		case Section:
			collectDependencies(node.Body, dependencies)
		}
	}
}

func normalizeViewName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "/"))
	if strings.HasSuffix(name, ".blade.php") {
		return name
	}
	if !strings.Contains(name, "/") {
		name = strings.ReplaceAll(name, ".", "/")
	}
	return name + ".blade.php"
}

func directiveWord(s string) string {
	i := 0
	for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || s[i] == '_') {
		i++
	}
	return s[:i]
}

func parsePath(raw string) ([]string, error) {
	if !pathRE.MatchString(raw) {
		return nil, fmt.Errorf("unsupported expression %q", raw)
	}
	return strings.Split(strings.TrimPrefix(raw, "$"), "->"), nil
}

func diagnostic(name, source string, at int, format string, args ...any) error {
	line, column := 1, 1
	for i, r := range source[:at] {
		if r == '\n' {
			line++
			column = 1
		} else if i < at {
			column++
		}
	}
	return fmt.Errorf("%s:%d:%d: %s", name, line, column, fmt.Sprintf(format, args...))
}

func smallestPositive(values ...int) int {
	best := -1
	for _, v := range values {
		if v >= 0 && (best < 0 || v < best) {
			best = v
		}
	}
	return best
}
