package squeeze

import (
	"go/ast"
	"go/token"
	"strings"
)

// RBACRoleSet represents roles defined in a project for RBAC analysis.
type RBACRoleSet struct {
	Defined map[string]bool // all roles that have been defined
	Removed map[string]bool // roles that were defined then removed
}

// RBACDefault represents a role marked as Default().
type RBACDefault struct {
	Role string
	File string
	Line int
}

// RoleHit represents a found role annotation in source code.
type RoleHit struct {
	Role string
	Line int
}

// ruleStaleRoleAnnotation flags migrations that use XxxSees() for a removed role.
func ruleStaleRoleAnnotation(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if ctx.RBACRoles == nil {
		return nil
	}

	for _, m := range ctx.Methods {
		hits := findSeesCallsForRoles(m.Body, m.Fset, ctx.RBACRoles.Removed)
		for _, hit := range hits {
			findings = append(findings, Finding{
				Rule:     "stale_role_annotation",
				Severity: SeverityWarning,
				File:     m.File,
				Line:     hit.Line,
				Message:  hit.Role + "Sees() references removed role \"" + hit.Role + "\" — update or remove the annotation",
			})
		}
	}

	return findings
}

// ruleUnknownRoleAnnotation flags migrations that use XxxSees() for a role that was never defined.
func ruleUnknownRoleAnnotation(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if ctx.RBACRoles == nil {
		return nil
	}

	for _, m := range ctx.Methods {
		hits := findSeesCallsForUndefined(m.Body, m.Fset, ctx.RBACRoles.Defined, ctx.RBACRoles.Removed)
		for _, hit := range hits {
			findings = append(findings, Finding{
				Rule:     "unknown_role_annotation",
				Severity: SeverityError,
				File:     m.File,
				Line:     hit.Line,
				Message:  hit.Role + "Sees() references undefined role \"" + hit.Role + "\" — define it or fix the typo",
			})
		}
	}

	return findings
}

// ruleRoleWithoutLoad flags routes that use RequireRole middleware without LoadRoles in the chain.
func ruleRoleWithoutLoad(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		hasRequireRole := false
		hasLoadRoles := false
		for _, mw := range route.Middleware {
			if strings.HasPrefix(mw, "RequireRole") {
				hasRequireRole = true
			}
			if mw == "LoadRoles" {
				hasLoadRoles = true
			}
		}

		if hasRequireRole && !hasLoadRoles {
			findings = append(findings, Finding{
				Rule:     "role_without_load",
				Severity: SeverityError,
				File:     route.File,
				Line:     route.Line,
				Message:  route.Method + " " + route.Path + " — RequireRole used without LoadRoles in middleware chain",
			})
		}
	}

	return findings
}

// ruleDefaultRoleMissing flags when roles are defined but no role has Default(), or multiple do.
func ruleDefaultRoleMissing(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if ctx.RBACRoles == nil || len(ctx.RBACRoles.Defined) == 0 {
		return nil
	}

	defaults := ctx.RBACDefaults
	if len(defaults) == 0 {
		findings = append(findings, Finding{
			Rule:     "default_role_missing",
			Severity: SeverityError,
			File:     "config/roles.go",
			Line:     1,
			Message:  "RBAC roles defined but no role has Default() — exactly one role must be the default",
		})
	} else if len(defaults) > 1 {
		for _, d := range defaults {
			findings = append(findings, Finding{
				Rule:     "default_role_missing",
				Severity: SeverityError,
				File:     d.File,
				Line:     d.Line,
				Message:  "multiple roles marked as Default() — \"" + d.Role + "\" conflicts with other defaults, only one is allowed",
			})
		}
	}

	return findings
}

// rulePreBirthAnnotation flags migrations that use XxxSees() for a role whose birth policy
// timestamp is after the migration timestamp. This happens when a role is dropped and recreated.
func rulePreBirthAnnotation(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if ctx.RBACRoles == nil || len(ctx.RoleBirths) == 0 {
		return nil
	}

	for _, m := range ctx.Methods {
		migTimestamp := extractTimestamp(m.File)
		if migTimestamp == "" {
			continue
		}
		hits := findSeesCallsForDefined(m.Body, m.Fset, ctx.RBACRoles.Defined)
		for _, hit := range hits {
			birth, ok := ctx.RoleBirths[hit.Role]
			if !ok {
				continue
			}
			if migTimestamp < birth {
				findings = append(findings, Finding{
					Rule:     "pre_birth_annotation",
					Severity: SeverityWarning,
					File:     m.File,
					Line:     hit.Line,
					Message:  hit.Role + "Sees() predates role \"" + hit.Role + "\" birth policy " + birth + " — annotation has no effect",
				})
			}
		}
	}

	return findings
}

// ruleMissingVisibilityScope flags controllers behind LoadRoles that query a model with
// visibility annotations but don't call SelectForRoles/SelectForRole/SelectAll.
func ruleMissingVisibilityScope(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if ctx.RBACRoles == nil || len(ctx.TablesWithVisibility) == 0 {
		return nil
	}

	for _, route := range ctx.Routes {
		hasLoadRoles := false
		for _, mw := range route.Middleware {
			if mw == "LoadRoles" {
				hasLoadRoles = true
				break
			}
		}
		if !hasLoadRoles {
			continue
		}

		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		// Check if the method has a Query call for a table with visibility annotations
		if hasQueryForVisibleTable(method.Body, ctx.TablesWithVisibility) && !hasVisibilityCall(method.Body) {
			findings = append(findings, Finding{
				Rule:     "missing_visibility_scope",
				Severity: SeverityError,
				File:     method.File,
				Line:     method.Line,
				Message:  route.Method + " " + route.Path + " — query has no visibility scope — call SelectForRoles(ctx.Roles()) or SelectAll()",
			})
		}
	}

	return findings
}

// ruleHardcodedRoleSelect flags SelectFor("literal") calls in controller code.
func ruleHardcodedRoleSelect(ctx *AnalysisContext) []Finding {
	var findings []Finding

	if ctx.RBACRoles == nil {
		return nil
	}

	for _, m := range ctx.Methods {
		lines := findHardcodedSelectFor(m.Body, m.Fset)
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "hardcoded_role_select",
				Severity: SeverityError,
				File:     m.File,
				Line:     line,
				Message:  "SelectFor() uses hardcoded role string — use ctx.Role() or ctx.Roles()",
			})
		}
	}

	return findings
}

// extractTimestamp extracts a migration timestamp from a file path.
// e.g. "database/migrations/2026_04_10_create_posts.go" -> "2026_04_10"
func extractTimestamp(path string) string {
	// Find the filename part
	name := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		name = path[idx+1:]
	}
	if idx := strings.LastIndex(name, "\\"); idx >= 0 {
		name = name[idx+1:]
	}
	// Extract leading timestamp (digits and underscores)
	i := 0
	digitCount := 0
	for i < len(name) && (name[i] >= '0' && name[i] <= '9' || name[i] == '_') {
		if name[i] >= '0' && name[i] <= '9' {
			digitCount++
		}
		i++
	}
	if digitCount >= 4 && i > 0 {
		ts := strings.TrimRight(name[:i], "_")
		return ts
	}
	return ""
}

// findSeesCallsForDefined scans for XxxSees() calls where Xxx is in the defined set.
func findSeesCallsForDefined(body *ast.BlockStmt, fset *token.FileSet, defined map[string]bool) []RoleHit {
	var hits []RoleHit
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if strings.HasSuffix(name, "Sees") {
			role := strings.TrimSuffix(name, "Sees")
			if role != "" && defined[role] {
				hits = append(hits, RoleHit{Role: role, Line: fset.Position(call.Pos()).Line})
			}
		}
		return true
	})
	return hits
}

// hasQueryForVisibleTable checks if a method body contains a Query* call for a table
// that has visibility annotations.
func hasQueryForVisibleTable(body *ast.BlockStmt, visibleTables map[string]bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if strings.HasPrefix(name, "Query") && len(name) > 5 {
			model := name[5:] // e.g. "Post" from "QueryPost"
			if visibleTables[model] {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// hasVisibilityCall checks if a method body calls SelectForRoles, SelectForRole, or SelectAll.
func hasVisibilityCall(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if name == "SelectForRoles" || name == "SelectForRole" || name == "SelectAll" {
			found = true
			return false
		}
		return true
	})
	return found
}

// findHardcodedSelectFor finds SelectFor("literal") calls and returns their line numbers.
func findHardcodedSelectFor(body *ast.BlockStmt, fset *token.FileSet) []int {
	var lines []int
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if name == "SelectFor" && len(call.Args) > 0 {
			// Check if the argument is a string literal
			if _, ok := call.Args[0].(*ast.BasicLit); ok {
				lines = append(lines, fset.Position(call.Pos()).Line)
			}
		}
		return true
	})
	return lines
}

// findSeesCallsForRoles scans an AST block for XxxSees() calls where Xxx is in the given role set.
func findSeesCallsForRoles(body *ast.BlockStmt, fset *token.FileSet, roles map[string]bool) []RoleHit {
	if len(roles) == 0 {
		return nil
	}
	var hits []RoleHit
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if strings.HasSuffix(name, "Sees") {
			role := strings.TrimSuffix(name, "Sees")
			if roles[role] {
				hits = append(hits, RoleHit{Role: role, Line: fset.Position(call.Pos()).Line})
			}
		}
		return true
	})
	return hits
}

// findSeesCallsForUndefined scans for XxxSees() calls where Xxx is not in defined or removed.
func findSeesCallsForUndefined(body *ast.BlockStmt, fset *token.FileSet, defined, removed map[string]bool) []RoleHit {
	var hits []RoleHit
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if strings.HasSuffix(name, "Sees") {
			role := strings.TrimSuffix(name, "Sees")
			if role != "" && !defined[role] && !removed[role] {
				hits = append(hits, RoleHit{Role: role, Line: fset.Position(call.Pos()).Line})
			}
		}
		return true
	})
	return hits
}

// callName extracts the function/method name from a call expression.
// For ident calls like Foo(), returns "Foo".
// For selector calls like x.Foo(), returns "Foo".
func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}
