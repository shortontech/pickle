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

	"github.com/shortontech/pickle/pkg/schema"
)

// ParsedRowPolicyFile is the normalized row-policy contribution from one file.
type ParsedRowPolicyFile struct {
	PolicyID   string
	SourceFile string
	Identities []schema.PolicyIdentityDefinition
	Operations []schema.RowPolicyOperation
}

// ResolvedRowPolicy is the replayed, schema-validated state for one table.
type ResolvedRowPolicy struct {
	Protection       schema.RowProtection
	SourcePolicies   []string
	EnforcementClass string // portable, application_only
	PhysicalPlans    map[string]string
	Identities       map[string]schema.PolicyIdentityType
}

func ParseRowPolicyOps(policiesDir string) ([]ParsedRowPolicyFile, error) {
	entries, err := os.ReadDir(policiesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ParsedRowPolicyFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") || strings.HasSuffix(entry.Name(), "_gen.go") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".go")
		if !migrationTimestamp.MatchString(stem) {
			continue
		}
		parsed, err := parseRowPolicyFile(filepath.Join(policiesDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parsing row policy %s: %w", entry.Name(), err)
		}
		if len(parsed.Operations) == 0 && len(parsed.Identities) == 0 {
			continue
		}
		parsed.PolicyID, parsed.SourceFile = stem, filepath.Join("database", "policies", entry.Name())
		out = append(out, parsed)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PolicyID < out[j].PolicyID })
	return out, nil
}

func parseRowPolicyFile(path string) (ParsedRowPolicyFile, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return ParsedRowPolicyFile{}, err
	}
	var out ParsedRowPolicyFile
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Up" || fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			exprStmt, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := exprStmt.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			switch sel.Sel.Name {
			case "IdentityUUID", "IdentityString", "IdentityStrings":
				if len(call.Args) != 1 {
					return out, fmt.Errorf("%s requires one string argument", sel.Sel.Name)
				}
				name := extractStringLit(call.Args[0])
				if name == "" {
					return out, fmt.Errorf("%s identity name must be a string literal", sel.Sel.Name)
				}
				kind := schema.PolicyIdentityUUID
				if sel.Sel.Name == "IdentityString" {
					kind = schema.PolicyIdentityString
				}
				if sel.Sel.Name == "IdentityStrings" {
					kind = schema.PolicyIdentityStrings
				}
				out.Identities = append(out.Identities, schema.PolicyIdentityDefinition{Name: name, Type: kind})
			case "Protect", "AlterProtection":
				op, err := parseProtectionCall(call, strings.ToLower(strings.TrimSuffix(sel.Sel.Name, "Protection")))
				if sel.Sel.Name == "AlterProtection" {
					op.Type = "alter_protection"
				}
				if err != nil {
					return out, err
				}
				out.Operations = append(out.Operations, op)
			case "Unprotect":
				if len(call.Args) != 1 {
					return out, fmt.Errorf("Unprotect requires one table")
				}
				table := extractStringLit(call.Args[0])
				if table == "" {
					return out, fmt.Errorf("Unprotect table must be a string literal")
				}
				out.Operations = append(out.Operations, schema.RowPolicyOperation{Type: "unprotect", Protection: schema.RowProtection{Table: table}})
			}
		}
	}
	return out, nil
}

func parseProtectionCall(call *ast.CallExpr, kind string) (schema.RowPolicyOperation, error) {
	if len(call.Args) != 2 {
		return schema.RowPolicyOperation{}, fmt.Errorf("Protect requires table and callback")
	}
	table := extractStringLit(call.Args[0])
	if table == "" {
		return schema.RowPolicyOperation{}, fmt.Errorf("Protect table must be a string literal")
	}
	fn, ok := call.Args[1].(*ast.FuncLit)
	if !ok || fn.Body == nil {
		return schema.RowPolicyOperation{}, fmt.Errorf("Protect callback must be a function literal")
	}
	p := schema.RowProtection{Table: table, SubjectCombination: schema.AnyOfSubjects}
	for _, stmt := range fn.Body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		calls := collectCallChain(call)
		if len(calls) == 0 {
			continue
		}
		rootSel, ok := calls[0].Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		switch rootSel.Sel.Name {
		case "CombineSubjects":
			if len(calls[0].Args) != 1 {
				return schema.RowPolicyOperation{}, fmt.Errorf("CombineSubjects requires a mode")
			}
			id, ok := calls[0].Args[0].(*ast.Ident)
			if !ok {
				return schema.RowPolicyOperation{}, fmt.Errorf("CombineSubjects mode must be AnyOfSubjects or AllOfSubjects")
			}
			if id.Name == "AllOfSubjects" {
				p.SubjectCombination = schema.AllOfSubjects
			} else if id.Name != "AnyOfSubjects" {
				return schema.RowPolicyOperation{}, fmt.Errorf("unknown subject combination %s", id.Name)
			}
		case "AllowApplicationOnly":
			if len(calls[0].Args) != 1 {
				return schema.RowPolicyOperation{}, fmt.Errorf("AllowApplicationOnly requires a reason")
			}
			reason := extractStringLit(calls[0].Args[0])
			if reason == "" {
				return schema.RowPolicyOperation{}, fmt.Errorf("application-only reason must be a literal")
			}
			p.ApplicationOnlyReasons = append(p.ApplicationOnlyReasons, reason)
		case "Rule":
			rule, err := parseRowRuleChain(calls)
			if err != nil {
				return schema.RowPolicyOperation{}, err
			}
			p.Rules = append(p.Rules, rule)
		}
	}
	return schema.RowPolicyOperation{Type: kind, Protection: p}, nil
}

func parseRowRuleChain(calls []*ast.CallExpr) (schema.RowRule, error) {
	if len(calls[0].Args) != 1 {
		return schema.RowRule{}, fmt.Errorf("Rule requires a stable key")
	}
	rule := schema.RowRule{Key: extractStringLit(calls[0].Args[0])}
	if rule.Key == "" {
		return rule, fmt.Errorf("Rule key must be a string literal")
	}
	for _, call := range calls[1:] {
		sel := call.Fun.(*ast.SelectorExpr)
		switch sel.Sel.Name {
		case "ForPublic":
			rule.Subject = schema.RowSubject{Kind: schema.SubjectPublic}
		case "ForAuthenticated":
			rule.Subject = schema.RowSubject{Kind: schema.SubjectAuthenticated}
		case "ForRole":
			if len(call.Args) != 1 || extractStringLit(call.Args[0]) == "" {
				return rule, fmt.Errorf("ForRole requires a role literal")
			}
			rule.Subject = schema.RowSubject{Kind: schema.SubjectRole, Name: extractStringLit(call.Args[0])}
		case "Select", "Insert", "Delete", "All":
			if len(call.Args) != 1 {
				return rule, fmt.Errorf("%s requires one predicate", sel.Sel.Name)
			}
			pred, err := parseRowPredicateExpr(call.Args[0])
			if err != nil {
				return rule, err
			}
			switch sel.Sel.Name {
			case "Select":
				rule.Select = &pred
			case "Insert":
				rule.Insert = &pred
			case "Delete":
				rule.Delete = &pred
			case "All":
				rule.Select, rule.Insert, rule.UpdateOld, rule.UpdateNew, rule.Delete = cloneParsedPredicate(pred), cloneParsedPredicate(pred), cloneParsedPredicate(pred), cloneParsedPredicate(pred), cloneParsedPredicate(pred)
			}
		case "Update":
			if len(call.Args) != 2 {
				return rule, fmt.Errorf("Update requires Existing and Proposed")
			}
			old, pos, err := parsePositionedPredicate(call.Args[0])
			if err != nil || pos != "existing" {
				return rule, fmt.Errorf("Update first argument must be Existing: %w", err)
			}
			newPred, pos, err := parsePositionedPredicate(call.Args[1])
			if err != nil || pos != "proposed" {
				return rule, fmt.Errorf("Update second argument must be Proposed: %w", err)
			}
			rule.UpdateOld, rule.UpdateNew = &old, &newPred
		}
	}
	return rule, nil
}

func cloneParsedPredicate(p schema.RowPredicate) *schema.RowPredicate { c := p; return &c }
func parsePositionedPredicate(expr ast.Expr) (schema.RowPredicate, string, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return schema.RowPredicate{}, "", fmt.Errorf("not a call")
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || (id.Name != "Existing" && id.Name != "Proposed") || len(call.Args) != 1 {
		return schema.RowPredicate{}, "", fmt.Errorf("invalid positioned predicate")
	}
	p, err := parseRowPredicateExpr(call.Args[0])
	return p, strings.ToLower(id.Name), err
}

func parseRowPredicateExpr(expr ast.Expr) (schema.RowPredicate, error) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return schema.RowPredicate{}, fmt.Errorf("row predicate must be a constructor call")
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return schema.RowPredicate{}, fmt.Errorf("row predicate constructor must be unqualified")
	}
	children := func() ([]schema.RowPredicate, error) {
		out := make([]schema.RowPredicate, 0, len(call.Args))
		for _, arg := range call.Args {
			p, err := parseRowPredicateExpr(arg)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	}
	switch id.Name {
	case "Allow":
		return schema.RowPredicate{Kind: schema.PredicateAllow}, nil
	case "Deny":
		return schema.RowPredicate{Kind: schema.PredicateDeny}, nil
	case "Identity", "PolicyColumn":
		if len(call.Args) != 1 || extractStringLit(call.Args[0]) == "" {
			return schema.RowPredicate{}, fmt.Errorf("%s requires a literal name", id.Name)
		}
		kind := schema.PredicateIdentity
		if id.Name == "PolicyColumn" {
			kind = schema.PredicateColumn
		}
		return schema.RowPredicate{Kind: kind, Name: extractStringLit(call.Args[0])}, nil
	case "Owner":
		if len(call.Args) != 2 || extractStringLit(call.Args[0]) == "" {
			return schema.RowPredicate{}, fmt.Errorf("Owner requires column literal and identity")
		}
		right, err := parseRowPredicateExpr(call.Args[1])
		if err != nil {
			return schema.RowPredicate{}, err
		}
		return schema.RowPredicate{Kind: schema.PredicateEqual, Children: []schema.RowPredicate{{Kind: schema.PredicateColumn, Name: extractStringLit(call.Args[0])}, right}}, nil
	case "Equal", "NotEqual", "And", "Or", "Not":
		ch, err := children()
		if err != nil {
			return schema.RowPredicate{}, err
		}
		kinds := map[string]schema.RowPredicateKind{"Equal": schema.PredicateEqual, "NotEqual": schema.PredicateNotEqual, "And": schema.PredicateAnd, "Or": schema.PredicateOr, "Not": schema.PredicateNot}
		return schema.RowPredicate{Kind: kinds[id.Name], Children: ch}, nil
	default:
		return schema.RowPredicate{}, fmt.Errorf("unsupported row predicate %s", id.Name)
	}
}

// ResolveRowPolicies replays definitions and validates references against schema and roles.
func ResolveRowPolicies(files []ParsedRowPolicyFile, tables []*schema.Table, roles []DerivedRole) ([]ResolvedRowPolicy, error) {
	tableMap := map[string]*schema.Table{}
	for _, table := range tables {
		tableMap[table.Name] = table
	}
	roleMap := map[string]bool{}
	for _, role := range roles {
		roleMap[role.Slug] = true
	}
	identities := map[string]schema.PolicyIdentityType{}
	state := map[string]*ResolvedRowPolicy{}
	var order []string
	for _, file := range files {
		for _, identity := range file.Identities {
			if previous, ok := identities[identity.Name]; ok && previous != identity.Type {
				return nil, fmt.Errorf("policy identity %q changes type", identity.Name)
			}
			identities[identity.Name] = identity.Type
		}
		for _, op := range file.Operations {
			table := tableMap[op.Protection.Table]
			if table == nil {
				return nil, fmt.Errorf("row policy %s references unknown table %q", file.PolicyID, op.Protection.Table)
			}
			if op.Type == "unprotect" {
				delete(state, op.Protection.Table)
				continue
			}
			if err := validateResolvedProtection(op.Protection, table, identities, roleMap); err != nil {
				return nil, fmt.Errorf("row policy %s: %w", file.PolicyID, err)
			}
			resolved := state[table.Name]
			if resolved == nil {
				resolved = &ResolvedRowPolicy{PhysicalPlans: map[string]string{}}
				state[table.Name] = resolved
				order = append(order, table.Name)
			}
			if op.Type == "protect" && len(resolved.Protection.Rules) > 0 {
				return nil, fmt.Errorf("table %q is already protected; use AlterProtection", table.Name)
			}
			resolved.Protection = op.Protection
			resolved.Identities = copyPolicyIdentities(identities)
			resolved.SourcePolicies = append(resolved.SourcePolicies, file.PolicyID)
			resolved.EnforcementClass = "portable"
			resolved.PhysicalPlans = rowPolicyPhysicalPlans(table, op.Protection)
			for _, plan := range resolved.PhysicalPlans {
				if strings.Contains(plan, "application_only") {
					resolved.EnforcementClass = "application_only"
				}
			}
			if resolved.EnforcementClass == "application_only" && !containsString(op.Protection.ApplicationOnlyReasons, "non_bijective_physical_plan") {
				return nil, fmt.Errorf("table %q requires AllowApplicationOnly(\"non_bijective_physical_plan\")", table.Name)
			}
		}
	}
	var out []ResolvedRowPolicy
	for _, table := range order {
		if policy := state[table]; policy != nil {
			out = append(out, *policy)
		}
	}
	return out, nil
}

func copyPolicyIdentities(in map[string]schema.PolicyIdentityType) map[string]schema.PolicyIdentityType {
	out := make(map[string]schema.PolicyIdentityType, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func validateResolvedProtection(p schema.RowProtection, table *schema.Table, identities map[string]schema.PolicyIdentityType, roles map[string]bool) error {
	cols := map[string]schema.ColumnType{}
	for _, col := range table.Columns {
		cols[col.Name] = col.Type
	}
	seen := map[string]bool{}
	for _, rule := range p.Rules {
		if rule.Key == "" || seen[rule.Key] {
			return fmt.Errorf("duplicate or empty rule key %q", rule.Key)
		}
		seen[rule.Key] = true
		if rule.Subject.Kind == schema.SubjectRole && !roles[rule.Subject.Name] {
			return fmt.Errorf("rule %q references unknown role %q", rule.Key, rule.Subject.Name)
		}
		for _, pred := range []*schema.RowPredicate{rule.Select, rule.Insert, rule.UpdateOld, rule.UpdateNew, rule.Delete} {
			if pred != nil {
				if err := validateResolvedPredicate(*pred, cols, identities); err != nil {
					return fmt.Errorf("rule %q: %w", rule.Key, err)
				}
			}
		}
		if (rule.UpdateOld == nil) != (rule.UpdateNew == nil) {
			return fmt.Errorf("rule %q update requires existing and proposed predicates", rule.Key)
		}
	}
	return nil
}

func validateResolvedPredicate(p schema.RowPredicate, cols map[string]schema.ColumnType, identities map[string]schema.PolicyIdentityType) error {
	if p.Kind == schema.PredicateColumn {
		if _, ok := cols[p.Name]; !ok {
			return fmt.Errorf("unknown column %q", p.Name)
		}
	}
	if p.Kind == schema.PredicateIdentity {
		if _, ok := identities[p.Name]; !ok {
			return fmt.Errorf("unknown identity %q", p.Name)
		}
	}
	for _, child := range p.Children {
		if err := validateResolvedPredicate(child, cols, identities); err != nil {
			return err
		}
	}
	if (p.Kind == schema.PredicateEqual || p.Kind == schema.PredicateNotEqual) && len(p.Children) == 2 {
		left, right := p.Children[0], p.Children[1]
		if left.Kind == schema.PredicateColumn && right.Kind == schema.PredicateIdentity {
			if !rowPolicyTypesCompatible(cols[left.Name], identities[right.Name]) {
				return fmt.Errorf("column %q and identity %q have incompatible types", left.Name, right.Name)
			}
		}
	}
	return nil
}

func rowPolicyTypesCompatible(column schema.ColumnType, identity schema.PolicyIdentityType) bool {
	return (column == schema.UUID && identity == schema.PolicyIdentityUUID) || ((column == schema.String || column == schema.Text) && identity == schema.PolicyIdentityString)
}
func rowPolicyPhysicalPlans(table *schema.Table, p schema.RowProtection) map[string]string {
	plans := map[string]string{}
	for _, rule := range p.Rules {
		if rule.Select != nil {
			plans["select"] = "select"
		}
		if rule.Insert != nil {
			plans["insert"] = "insert"
		}
		if rule.UpdateOld != nil {
			if table.IsImmutable {
				plans["update"] = "insert:application_only"
			} else {
				plans["update"] = "update"
			}
		}
		if rule.Delete != nil {
			if table.IsImmutable {
				plans["delete"] = "insert:application_only"
			} else if table.HasSoftDelete {
				plans["delete"] = "update:application_only"
			} else {
				plans["delete"] = "delete"
			}
		}
	}
	return plans
}
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
