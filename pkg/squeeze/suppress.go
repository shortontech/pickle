package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// suppressionDirective is the comment prefix used to silence a squeeze rule on
// the following declaration. Usage:
//
//	//squeeze:ignore <rule-name> [optional reason]
//	func (c UserController) Session(ctx *pickle.Context) { ... }
//
// A directive MUST name a specific rule — a blanket ignore is not supported.
const suppressionDirective = "squeeze:ignore"

// suppression records a single //squeeze:ignore directive: which rule it silences
// and the source range of the declaration it annotates. A finding is suppressed
// when it matches the file, the rule name, and its line falls within [Start, End].
type suppression struct {
	Rule  string
	File  string
	Start int
	End   int
}

// parseSuppressionDirective extracts the rule name from a single comment line if
// it is a //squeeze:ignore directive. It returns (rule, true) when the comment is
// a directive that names a rule, and ("", false) otherwise. Both //squeeze:ignore
// and "// squeeze:ignore" (with a leading space) are accepted.
func parseSuppressionDirective(commentText string) (string, bool) {
	text := strings.TrimSpace(strings.TrimPrefix(commentText, "//"))
	if !strings.HasPrefix(text, suppressionDirective) {
		return "", false
	}
	fields := strings.Fields(text)
	// fields[0] == "squeeze:ignore"; a rule name is mandatory.
	if len(fields) < 2 || fields[0] != suppressionDirective {
		return "", false
	}
	return fields[1], true
}

// collectSuppressions parses the given Go source files (with comments) and returns
// every //squeeze:ignore directive attached to a function or method declaration,
// scoped to that declaration's source range. projectDir is used to resolve any
// file path that isn't directly readable relative to the current directory.
func collectSuppressions(files []string, projectDir string) []suppression {
	var sups []suppression
	for _, file := range files {
		path := resolveSuppressionPath(file, projectDir)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			start := fset.Position(fn.Pos()).Line
			end := fset.Position(fn.End()).Line
			for _, c := range fn.Doc.List {
				rule, ok := parseSuppressionDirective(c.Text)
				if !ok {
					continue
				}
				sups = append(sups, suppression{
					Rule:  rule,
					File:  file,
					Start: start,
					End:   end,
				})
			}
		}
	}
	return sups
}

// resolveSuppressionPath returns a readable path for file, falling back to
// projectDir-relative resolution when file isn't directly accessible.
func resolveSuppressionPath(file, projectDir string) string {
	if _, err := os.Stat(file); err == nil {
		return file
	}
	if projectDir != "" {
		if joined := filepath.Join(projectDir, file); joined != file {
			if _, err := os.Stat(joined); err == nil {
				return joined
			}
		}
	}
	return file
}

// applySuppressions partitions findings into those kept and those suppressed by a
// matching directive. A finding is suppressed when its file matches a suppression's
// file, its rule matches, and its line falls within the annotated declaration.
func applySuppressions(findings []Finding, sups []suppression) (kept []Finding, suppressed []Finding) {
	if len(sups) == 0 {
		return findings, nil
	}
	for _, f := range findings {
		if isSuppressed(f, sups) {
			suppressed = append(suppressed, f)
			continue
		}
		kept = append(kept, f)
	}
	return kept, suppressed
}

// isSuppressed reports whether a finding is silenced by any suppression directive.
func isSuppressed(f Finding, sups []suppression) bool {
	for _, s := range sups {
		if s.Rule != f.Rule {
			continue
		}
		if !sameFile(s.File, f.File) {
			continue
		}
		if f.Line >= s.Start && f.Line <= s.End {
			return true
		}
	}
	return false
}

// sameFile compares two file paths for equality, tolerating differences in
// cleaning (e.g. "./a/b.go" vs "a/b.go").
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// uniqueFindingFiles returns the distinct, non-empty Go source files referenced by
// a set of findings.
func uniqueFindingFiles(findings []Finding) []string {
	seen := make(map[string]bool)
	var files []string
	for _, f := range findings {
		if f.File == "" || !strings.HasSuffix(f.File, ".go") {
			continue
		}
		if seen[f.File] {
			continue
		}
		seen[f.File] = true
		files = append(files, f.File)
	}
	return files
}
