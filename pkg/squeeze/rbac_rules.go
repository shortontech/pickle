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
