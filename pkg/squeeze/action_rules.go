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
				Message:  "action method called directly in action package — dispatch through the gated model method instead",
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
				Message:  "action method called directly in action package — dispatch through the gated model method instead",
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

// ruleScopeSideEffect flags method calls in scope files that are not on the allowed
// ScopeBuilder method list. Scopes should only call filtering/scoping methods.
func ruleScopeSideEffect(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if len(ctx.ScopeAllowedMethods) == 0 {
		return nil
	}

	for _, m := range ctx.Methods {
		if !isScopeFile(m.File) {
			continue
		}

		lines := findDisallowedScopeCalls(m.Body, m.Fset, ctx.ScopeAllowedMethods)
		for _, hit := range lines {
			findings = append(findings, Finding{
				Rule:     "scope_side_effect",
				Severity: SeverityError,
				File:     m.File,
				Line:     hit.Line,
				Message:  hit.Role + "() is not available on ScopeBuilder — scopes must only filter, not execute",
			})
		}
	}

	for _, pf := range ctx.FuncRegistry {
		file := pf.Fset.Position(pf.Body.Pos()).Filename
		if !isScopeFile(file) {
			continue
		}

		lines := findDisallowedScopeCalls(pf.Body, pf.Fset, ctx.ScopeAllowedMethods)
		for _, hit := range lines {
			findings = append(findings, Finding{
				Rule:     "scope_side_effect",
				Severity: SeverityError,
				File:     file,
				Line:     hit.Line,
				Message:  hit.Role + "() is not available on ScopeBuilder — scopes must only filter, not execute",
			})
		}
	}

	return findings
}

// findDisallowedScopeCalls finds method calls on selector expressions that are not in the
// allowed set. Returns RoleHit where Role is actually the method name (reusing the struct).
func findDisallowedScopeCalls(body *ast.BlockStmt, fset *token.FileSet, allowed map[string]bool) []RoleHit {
	var hits []RoleHit
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		methodName := sel.Sel.Name
		// Only flag methods that look like they're on a builder (selector calls)
		// and are not in the allowed list
		if !allowed[methodName] {
			hits = append(hits, RoleHit{Role: methodName, Line: fset.Position(call.Pos()).Line})
		}
		return true
	})
	return hits
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

// findDirectActionCalls finds direct method calls on Action types within
// action files. The convention is that XxxAction.Xxx() is the action method,
// and the generator renames it to lowercase. Calling it directly bypasses the gate.
// We detect any lowercase method call on a variable whose selector starts with
// a lowercase letter — matching the renamed pattern.
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
		// Flag calls to lowercase methods on "action" receivers — these are
		// the renamed action methods that should only be called through the gate.
		methodName := sel.Sel.Name
		if len(methodName) > 0 && methodName[0] >= 'a' && methodName[0] <= 'z' {
			// Check if the receiver looks like an action variable
			if ident, ok := sel.X.(*ast.Ident); ok {
				if ident.Name == "action" || strings.HasSuffix(ident.Name, "Action") {
					lines = append(lines, fset.Position(call.Pos()).Line)
				}
			}
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
