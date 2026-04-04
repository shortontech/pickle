package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StaticRoleOp represents a role operation extracted from the AST of a policy file.
type StaticRoleOp struct {
	Type          string   // "create", "alter", "drop"
	Slug          string
	DisplayName   string
	IsManages     bool
	RemoveManages bool
	IsDefault     bool
	RemoveDefault bool
	Actions       []string
	RevokeActions []string
}

// StaticPolicyOps groups role operations by the policy file they came from.
type StaticPolicyOps struct {
	PolicyID   string // migration-style ID from filename
	SourceFile string // relative path
	Ops        []StaticRoleOp
}

// DerivedRole represents the computed state of a role after replaying all policies.
type DerivedRole struct {
	Slug           string
	DisplayName    string
	IsManages      bool
	IsDefault      bool
	Actions        []string
	BirthTimestamp string // policy ID that created this role
}

// ActionGrant maps an action name to the roles that can perform it plus manages roles.
type ActionGrant struct {
	ActionName   string
	AllowedRoles []string // roles with explicit Can() for this action
	ManagesRoles []string // roles with Manages()
	SourcePolicy string   // policy that last granted this action
}

// ParsePolicyOps statically parses all policy files in the given directory,
// extracting role operations from the Up() methods via AST analysis.
func ParsePolicyOps(policiesDir string) ([]StaticPolicyOps, error) {
	entries, err := os.ReadDir(policiesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var allPolicies []StaticPolicyOps

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") || strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".go")
		if !migrationTimestamp.MatchString(stem) {
			continue
		}

		filePath := filepath.Join(policiesDir, e.Name())
		ops, err := parsePolicyFileOps(filePath)
		if err != nil {
			return nil, fmt.Errorf("parsing policy %s: %w", e.Name(), err)
		}
		if len(ops) == 0 {
			continue
		}

		allPolicies = append(allPolicies, StaticPolicyOps{
			PolicyID:   stem,
			SourceFile: filepath.Join("database/policies", e.Name()),
			Ops:        ops,
		})
	}

	sort.Slice(allPolicies, func(i, j int) bool {
		return allPolicies[i].PolicyID < allPolicies[j].PolicyID
	})

	return allPolicies, nil
}

// parsePolicyFileOps parses a single policy file and extracts role operations from Up() methods.
func parsePolicyFileOps(filePath string) ([]StaticRoleOp, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, nil, 0)
	if err != nil {
		return nil, err
	}

	var ops []StaticRoleOp

	// Find all Up() methods
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Up" || fn.Recv == nil {
			continue
		}
		// Parse the statements in the Up() body
		if fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			exprStmt, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			op := parseRoleChain(exprStmt.X)
			if op != nil {
				ops = append(ops, *op)
			}
		}
	}

	return ops, nil
}

// parseRoleChain parses a chained call expression like:
//
//	m.CreateRole("admin").Name("Administrator").Manages().Can("x", "y")
//
// or m.DropRole("viewer")
func parseRoleChain(expr ast.Expr) *StaticRoleOp {
	// Collect the chain of method calls from outermost to innermost
	calls := collectCallChain(expr)
	if len(calls) == 0 {
		return nil
	}

	// The innermost call should be CreateRole, AlterRole, or DropRole
	root := calls[0]
	sel, ok := root.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	op := &StaticRoleOp{}

	switch sel.Sel.Name {
	case "CreateRole":
		op.Type = "create"
		if len(root.Args) >= 1 {
			op.Slug = extractStringLit(root.Args[0])
		}
	case "AlterRole":
		op.Type = "alter"
		if len(root.Args) >= 1 {
			op.Slug = extractStringLit(root.Args[0])
		}
	case "DropRole":
		op.Type = "drop"
		if len(root.Args) >= 1 {
			op.Slug = extractStringLit(root.Args[0])
		}
		return op
	default:
		return nil
	}

	if op.Slug == "" {
		return nil
	}

	// Process the rest of the chain (from index 1 onwards)
	for _, call := range calls[1:] {
		chainSel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		switch chainSel.Sel.Name {
		case "Name":
			if len(call.Args) >= 1 {
				op.DisplayName = extractStringLit(call.Args[0])
			}
		case "Manages":
			op.IsManages = true
		case "RemoveManages":
			op.RemoveManages = true
		case "Default":
			op.IsDefault = true
		case "RemoveDefault":
			op.RemoveDefault = true
		case "Can":
			for _, arg := range call.Args {
				if s := extractStringLit(arg); s != "" {
					op.Actions = append(op.Actions, s)
				}
			}
		case "RevokeCan":
			for _, arg := range call.Args {
				if s := extractStringLit(arg); s != "" {
					op.RevokeActions = append(op.RevokeActions, s)
				}
			}
		}
	}

	return op
}

// collectCallChain flattens a chain like a.B().C().D() into [a.B(), a.B().C(), a.B().C().D()]
// returned from innermost to outermost.
func collectCallChain(expr ast.Expr) []*ast.CallExpr {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}

	// Check if the function is a selector on another call (chained)
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	// If the receiver of the selector is itself a call, recurse
	if innerCall, ok := sel.X.(*ast.CallExpr); ok {
		inner := collectCallChain(innerCall)
		return append(inner, call)
	}

	// Base case: receiver is not a call (e.g., m.CreateRole)
	return []*ast.CallExpr{call}
}

// extractStringLit extracts the string value from a basic string literal AST node.
func extractStringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	// Remove surrounding quotes
	s := lit.Value
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return ""
}

// StaticDeriveRoles replays parsed policy operations to compute the current role set.
// Respects birth timestamps: a dropped and recreated role loses old Can() grants.
func StaticDeriveRoles(policies []StaticPolicyOps) []DerivedRole {
	roles := map[string]*DerivedRole{}
	var order []string

	for _, policy := range policies {
		for _, op := range policy.Ops {
			switch op.Type {
			case "create":
				roles[op.Slug] = &DerivedRole{
					Slug:           op.Slug,
					DisplayName:    op.DisplayName,
					IsManages:      op.IsManages,
					IsDefault:      op.IsDefault,
					Actions:        append([]string{}, op.Actions...),
					BirthTimestamp: policy.PolicyID,
				}
				order = append(order, op.Slug)

			case "alter":
				r := roles[op.Slug]
				if r == nil {
					continue
				}
				if op.DisplayName != "" {
					r.DisplayName = op.DisplayName
				}
				if op.IsManages {
					r.IsManages = true
				}
				if op.RemoveManages {
					r.IsManages = false
				}
				if op.IsDefault {
					r.IsDefault = true
				}
				if op.RemoveDefault {
					r.IsDefault = false
				}
				r.Actions = append(r.Actions, op.Actions...)
				for _, revoke := range op.RevokeActions {
					filtered := r.Actions[:0]
					for _, a := range r.Actions {
						if a != revoke {
							filtered = append(filtered, a)
						}
					}
					r.Actions = filtered
				}

			case "drop":
				delete(roles, op.Slug)
				for i, s := range order {
					if s == op.Slug {
						order = append(order[:i], order[i+1:]...)
						break
					}
				}
			}
		}
	}

	var result []DerivedRole
	for _, slug := range order {
		if r, ok := roles[slug]; ok {
			result = append(result, *r)
		}
	}
	return result
}

// DeriveActionGrants computes the action grants from derived roles.
// For each action that any role Can() do, it builds an ActionGrant with
// the allowed roles and manages roles.
func DeriveActionGrants(roles []DerivedRole) []ActionGrant {
	// Collect manages roles
	var managesRoles []string
	for _, r := range roles {
		if r.IsManages {
			managesRoles = append(managesRoles, r.Slug)
		}
	}

	// Collect action → roles mapping
	actionRoles := map[string][]string{}
	actionSource := map[string]string{} // last source policy for the action
	var actionOrder []string

	for _, r := range roles {
		for _, action := range r.Actions {
			if _, exists := actionRoles[action]; !exists {
				actionOrder = append(actionOrder, action)
			}
			actionRoles[action] = append(actionRoles[action], r.Slug)
			actionSource[action] = r.BirthTimestamp // approximate; the role's birth policy
		}
	}

	sort.Strings(actionOrder)

	var grants []ActionGrant
	for _, action := range actionOrder {
		grants = append(grants, ActionGrant{
			ActionName:   action,
			AllowedRoles: actionRoles[action],
			ManagesRoles: managesRoles,
		})
	}
	return grants
}

// GenerateRBACGates generates gate files for actions that lack user-written gates.
// It returns a map of filePath → generated content.
// actionsDir is the path to database/actions/.
// policiesDir is the path to database/policies/.
func GenerateRBACGates(actionsDir, policiesDir string) (map[string][]byte, error) {
	// 1. Parse policies statically
	policies, err := ParsePolicyOps(policiesDir)
	if err != nil {
		return nil, fmt.Errorf("parsing policy ops: %w", err)
	}
	if len(policies) == 0 {
		return nil, nil
	}

	// 2. Derive roles and action grants
	roles := StaticDeriveRoles(policies)
	grants := DeriveActionGrants(roles)

	// Build grant lookup by action name
	grantIndex := map[string]*ActionGrant{}
	for i := range grants {
		grantIndex[grants[i].ActionName] = &grants[i]
	}

	// Find the source policy for the grants (use last policy file that contributed)
	sourcePolicy := policies[len(policies)-1].SourceFile

	result := map[string][]byte{}

	// 3. Scan existing actions and gates, generate gates for ungated actions
	actionSets, err := ScanActions(actionsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("scanning actions: %w", err)
	}

	for modelName, set := range actionSets {
		// Build user-gate index for this model
		userGateActions := map[string]bool{}
		for _, g := range set.Gates {
			if !g.IsGenerated {
				userGateActions[g.ActionName] = true
			}
		}

		modelDir := filepath.Join(actionsDir, modelName)

		for _, action := range set.Actions {
			// Skip if user-written gate exists
			if userGateActions[action.Name] {
				continue
			}

			// Find matching grant from RBAC
			grant := grantIndex[action.Name]
			if grant == nil {
				continue // no RBAC grant for this action
			}

			gateFile := toSnakeCase(action.Name) + "_gate_gen.go"
			gatePath := filepath.Join(modelDir, gateFile)

			src, err := GenerateRBACGate(toSnakeCase(action.Name), grant.AllowedRoles, grant.ManagesRoles, sourcePolicy, modelName)
			if err != nil {
				return nil, fmt.Errorf("generating gate for %s.%s: %w", modelName, action.Name, err)
			}
			result[gatePath] = src
		}
	}

	// 4. Generate default AssignRole gate if no user override exists
	roleDir := filepath.Join(actionsDir, "role")
	if _, err := os.Stat(roleDir); os.IsNotExist(err) {
		if err := os.MkdirAll(roleDir, 0755); err != nil {
			return nil, fmt.Errorf("creating role actions dir: %w", err)
		}
	}

	assignGateUser := filepath.Join(roleDir, "assign_gate.go")
	if _, err := os.Stat(assignGateUser); os.IsNotExist(err) {
		assignGatePath := filepath.Join(roleDir, "assign_gate_gen.go")
		src, err := GenerateAssignRoleGate("role")
		if err != nil {
			return nil, fmt.Errorf("generating assign role gate: %w", err)
		}
		result[assignGatePath] = src
	}

	return result, nil
}

// toSnakeCase converts PascalCase or camelCase to snake_case.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

// GenerateAssignRoleGate produces the default AssignRole gate that requires ctx.IsAdmin().
func GenerateAssignRoleGate(packageName string) ([]byte, error) {
	src := fmt.Sprintf(`// Code generated by Pickle. DO NOT EDIT.
// Default gate: only admins can assign roles.
package %s

import "github.com/google/uuid"

// CanAssign checks whether the authenticated user is authorised to
// assign roles. Default: requires admin.
func CanAssign(ctx *Context, model interface{ OwnerID() string }) *uuid.UUID {
	if ctx.IsAdmin() {
		id := uuid.New()
		return &id
	}
	return nil
}
`, packageName)
	return []byte(src), nil
}
