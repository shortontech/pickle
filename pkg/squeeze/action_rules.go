package squeeze

import (
	"go/ast"
	"go/token"
	"strings"
)

// ruleUngatedAction flags action files in app/actions/ that have no corresponding gate file.
func ruleUngatedAction(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, action := range ctx.Actions {
		if !action.HasGate {
			findings = append(findings, Finding{
				Rule:     "ungated_action",
				Severity: SeverityError,
				File:     action.File,
				Line:     1,
				Message:  "action \"" + action.Name + "\" has no corresponding gate file — create " + action.GateFile + " to authorize execution",
			})
		}
	}

	return findings
}

// ruleDirectExecuteCall flags .execute() calls directly within the action package.
// Actions should be dispatched through the gate, not called directly.
func ruleDirectExecuteCall(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, m := range ctx.Methods {
		// Only check files inside an actions directory
		if !isActionFile(m.File) {
			continue
		}

		lines := findExecuteCalls(m.Body, m.Fset)
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "direct_execute_call",
				Severity: SeverityError,
				File:     m.File,
				Line:     line,
				Message:  ".Execute() called directly in action package — dispatch through the gate instead",
			})
		}
	}

	// Also check helper functions in the registry
	for _, pf := range ctx.FuncRegistry {
		file := pf.Fset.Position(pf.Body.Pos()).Filename
		if !isActionFile(file) {
			continue
		}

		lines := findExecuteCalls(pf.Body, pf.Fset)
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "direct_execute_call",
				Severity: SeverityError,
				File:     file,
				Line:     line,
				Message:  ".Execute() called directly in action package — dispatch through the gate instead",
			})
		}
	}

	return findings
}

// ruleScopeBuilderLeak flags ScopeBuilder references outside database/scopes/.
func ruleScopeBuilderLeak(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, m := range ctx.Methods {
		if isScopeFile(m.File) {
			continue
		}

		lines := findIdentRefs(m.Body, m.Fset, "ScopeBuilder")
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "scope_builder_leak",
				Severity: SeverityError,
				File:     m.File,
				Line:     line,
				Message:  "ScopeBuilder referenced outside database/scopes/ — scopes must stay in their package",
			})
		}
	}

	for _, pf := range ctx.FuncRegistry {
		file := pf.Fset.Position(pf.Body.Pos()).Filename
		if isScopeFile(file) {
			continue
		}

		lines := findIdentRefs(pf.Body, pf.Fset, "ScopeBuilder")
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "scope_builder_leak",
				Severity: SeverityError,
				File:     file,
				Line:     line,
				Message:  "ScopeBuilder referenced outside database/scopes/ — scopes must stay in their package",
			})
		}
	}

	return findings
}

// ruleQueryBuilderInScope flags XxxQuery references inside database/scopes/.
// Scopes should use the ScopeBuilder API, not model query builders directly.
func ruleQueryBuilderInScope(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, m := range ctx.Methods {
		if !isScopeFile(m.File) {
			continue
		}

		lines := findQueryRefs(m.Body, m.Fset)
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "query_builder_in_scope",
				Severity: SeverityError,
				File:     m.File,
				Line:     line,
				Message:  "model query builder referenced inside database/scopes/ — use ScopeBuilder API instead",
			})
		}
	}

	for _, pf := range ctx.FuncRegistry {
		file := pf.Fset.Position(pf.Body.Pos()).Filename
		if !isScopeFile(file) {
			continue
		}

		lines := findQueryRefs(pf.Body, pf.Fset)
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "query_builder_in_scope",
				Severity: SeverityError,
				File:     file,
				Line:     line,
				Message:  "model query builder referenced inside database/scopes/ — use ScopeBuilder API instead",
			})
		}
	}

	return findings
}

// ActionInfo describes an action file for squeeze analysis.
type ActionInfo struct {
	Name     string // action name, e.g. "CreateTransfer"
	File     string // path to the action file
	GateFile string // expected gate file path
	HasGate  bool   // whether the gate file exists
}

// isActionFile returns true if the file path is inside an actions directory.
func isActionFile(path string) bool {
	return strings.Contains(path, "/actions/") || strings.Contains(path, "\\actions\\")
}

// isScopeFile returns true if the file path is inside database/scopes/.
func isScopeFile(path string) bool {
	return strings.Contains(path, "database/scopes/") || strings.Contains(path, "database\\scopes\\")
}

// findExecuteCalls finds .Execute() method calls in an AST block.
func findExecuteCalls(body *ast.BlockStmt, fset *token.FileSet) []int {
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Execute" {
			lines = append(lines, fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}

// findIdentRefs finds references to a specific identifier name in an AST block.
func findIdentRefs(body *ast.BlockStmt, fset *token.FileSet, name string) []int {
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == name {
			lines = append(lines, fset.Position(ident.Pos()).Line)
		}
		return true
	})
	return lines
}

// findQueryRefs finds XxxQuery function calls (like QueryUser, QueryPost) in an AST block.
func findQueryRefs(body *ast.BlockStmt, fset *token.FileSet) []int {
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		}
		if strings.HasPrefix(name, "Query") && name != "Query" && len(name) > 5 {
			lines = append(lines, fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}
