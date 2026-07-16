package cooked

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/google/uuid"
)

var ErrPolicyContextRequired = errors.New("pickle: protected query requires policy context")

type PolicyContext struct {
	identities map[string]string
	roles      map[string]bool
}

// NewVerifiedPolicyContext is intended for generated trusted entry-point
// adapters. Squeeze rejects direct calls from ordinary application code.
func NewVerifiedPolicyContext(identities map[string]string, roles []string) PolicyContext {
	types := map[string]string{}
	for _, definition := range rowPolicyRuntimeRegistry {
		for name, kind := range definition.IdentityTypes {
			types[name] = kind
		}
	}
	copyIDs := make(map[string]string, len(identities))
	for key, value := range identities {
		kind, declared := types[key]
		if !declared || value == "" || len(value) > 65536 {
			continue
		}
		if kind == "uuid" {
			parsed, err := uuid.Parse(value)
			if err != nil {
				continue
			}
			value = parsed.String()
		}
		copyIDs[key] = value
	}
	roleSet := make(map[string]bool, len(roles))
	for _, role := range roles {
		if role != "" && len(role) <= 1024 {
			roleSet[role] = true
		}
	}
	return PolicyContext{identities: copyIDs, roles: roleSet}
}
func (c PolicyContext) identity(name string) (string, bool) {
	value, ok := c.identities[name]
	return value, ok && value != ""
}
func (c PolicyContext) hasRole(name string) bool { return c.roles[name] }
func (c PolicyContext) encodedRoles() string {
	values := make([]string, 0, len(c.roles))
	for role := range c.roles {
		values = append(values, role)
	}
	sort.Strings(values)
	data, _ := json.Marshal(values)
	return string(data)
}

type rowPolicyRuntimePredicate struct {
	Kind, Name string
	Children   []rowPolicyRuntimePredicate
}
type rowPolicyRuntimeRule struct {
	Key, SubjectKind, SubjectName                string
	Select, Insert, UpdateOld, UpdateNew, Delete *rowPolicyRuntimePredicate
}
type rowPolicyRuntimeDefinition struct {
	Table, SubjectCombination, EnforcementClass string
	IdentityTypes                               map[string]string
	Rules                                       []rowPolicyRuntimeRule
}

var rowPolicyRuntimeRegistry = map[string]rowPolicyRuntimeDefinition{}

func registerRowPolicyRuntime(definition rowPolicyRuntimeDefinition) {
	rowPolicyRuntimeRegistry[definition.Table] = definition
}

type rowPolicyState struct {
	context *PolicyContext
	clause  string
	args    []any
	err     error
}

func compileRowPolicy(table, operation, alias string, context *PolicyContext) (string, []any, error) {
	definition, protected := rowPolicyRuntimeRegistry[table]
	if !protected {
		return "", nil, nil
	}
	ctx := PolicyContext{identities: map[string]string{}, roles: map[string]bool{}}
	if context != nil {
		ctx = *context
	}
	var parts []string
	var args []any
	for _, rule := range definition.Rules {
		if !runtimeSubjectMatches(rule, ctx) {
			continue
		}
		var predicate *rowPolicyRuntimePredicate
		switch operation {
		case "select":
			predicate = rule.Select
		case "insert":
			predicate = rule.Insert
		case "update_old":
			predicate = rule.UpdateOld
		case "update_new":
			predicate = rule.UpdateNew
		case "delete":
			predicate = rule.Delete
		}
		if predicate == nil {
			continue
		}
		sql, values, err := compileRuntimePredicate(*predicate, alias, ctx)
		if err != nil {
			return "", nil, fmt.Errorf("row policy %s.%s: %w", table, operation, err)
		}
		parts = append(parts, "("+sql+")")
		args = append(args, values...)
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("%w for %s.%s", ErrPolicyContextRequired, table, operation)
	}
	join := " OR "
	if definition.SubjectCombination == "all" {
		join = " AND "
	}
	return "(" + strings.Join(parts, join) + ")", args, nil
}

func runtimeSubjectMatches(rule rowPolicyRuntimeRule, context PolicyContext) bool {
	switch rule.SubjectKind {
	case "public":
		return true
	case "authenticated":
		_, ok := context.identity("user_id")
		return ok
	case "role":
		return context.hasRole(rule.SubjectName)
	}
	return false
}
func compileRuntimePredicate(p rowPolicyRuntimePredicate, alias string, context PolicyContext) (string, []any, error) {
	switch p.Kind {
	case "allow":
		return "TRUE", nil, nil
	case "deny":
		return "FALSE", nil, nil
	case "column":
		prefix := ""
		if alias != "" {
			prefix = alias + "."
		}
		return prefix + quoteRuntimeIdent(p.Name), nil, nil
	case "identity":
		value, ok := context.identity(p.Name)
		if !ok {
			return "", nil, fmt.Errorf("missing identity %q", p.Name)
		}
		return "?", []any{value}, nil
	case "equal", "not_equal":
		if len(p.Children) != 2 {
			return "", nil, fmt.Errorf("invalid comparison")
		}
		left, la, err := compileRuntimePredicate(p.Children[0], alias, context)
		if err != nil {
			return "", nil, err
		}
		right, ra, err := compileRuntimePredicate(p.Children[1], alias, context)
		if err != nil {
			return "", nil, err
		}
		op := "="
		if p.Kind == "not_equal" {
			op = "<>"
		}
		return "COALESCE((" + left + " " + op + " " + right + "), FALSE)", append(la, ra...), nil
	case "and", "or":
		join := " AND "
		if p.Kind == "or" {
			join = " OR "
		}
		var parts []string
		var args []any
		for _, child := range p.Children {
			part, values, err := compileRuntimePredicate(child, alias, context)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, part)
			args = append(args, values...)
		}
		return "(" + strings.Join(parts, join) + ")", args, nil
	case "not":
		if len(p.Children) != 1 {
			return "", nil, fmt.Errorf("invalid not")
		}
		child, args, err := compileRuntimePredicate(p.Children[0], alias, context)
		return "COALESCE(NOT (" + child + "), FALSE)", args, err
	}
	return "", nil, fmt.Errorf("unknown predicate %q", p.Kind)
}
func quoteRuntimeIdent(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func bindRuntimeClause(clause string, start int) string {
	var b strings.Builder
	n := start
	for _, r := range clause {
		if r == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func evaluateRowPolicyRecord(table, operation string, context *PolicyContext, record any) error {
	definition, protected := rowPolicyRuntimeRegistry[table]
	if !protected {
		return nil
	}
	ctx := PolicyContext{identities: map[string]string{}, roles: map[string]bool{}}
	if context != nil {
		ctx = *context
	}
	matched := 0
	allowedCount := 0
	for _, rule := range definition.Rules {
		if !runtimeSubjectMatches(rule, ctx) {
			continue
		}
		var predicate *rowPolicyRuntimePredicate
		if operation == "insert" {
			predicate = rule.Insert
		} else {
			predicate = rule.UpdateNew
		}
		if predicate == nil {
			continue
		}
		matched++
		allowed, err := evaluateRuntimePredicate(*predicate, ctx, record)
		if err != nil {
			return err
		}
		if allowed {
			allowedCount++
			if definition.SubjectCombination != "all" {
				return nil
			}
		}
	}
	if matched > 0 && definition.SubjectCombination == "all" && allowedCount == matched {
		return nil
	}
	return fmt.Errorf("row policy denied %s.%s", table, operation)
}
func evaluateRuntimePredicate(p rowPolicyRuntimePredicate, context PolicyContext, record any) (bool, error) {
	switch p.Kind {
	case "allow":
		return true, nil
	case "deny":
		return false, nil
	case "equal", "not_equal":
		if len(p.Children) != 2 {
			return false, fmt.Errorf("invalid comparison")
		}
		left, err := runtimePredicateValue(p.Children[0], context, record)
		if err != nil {
			return false, err
		}
		right, err := runtimePredicateValue(p.Children[1], context, record)
		if err != nil {
			return false, err
		}
		if isRuntimePolicyNull(left) || isRuntimePolicyNull(right) {
			return false, nil
		}
		equal := fmt.Sprint(left) == fmt.Sprint(right)
		if p.Kind == "not_equal" {
			equal = !equal
		}
		return equal, nil
	case "and":
		for _, child := range p.Children {
			ok, err := evaluateRuntimePredicate(child, context, record)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case "or":
		for _, child := range p.Children {
			ok, err := evaluateRuntimePredicate(child, context, record)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "not":
		ok, err := evaluateRuntimePredicate(p.Children[0], context, record)
		return !ok, err
	}
	return false, fmt.Errorf("predicate %q is not boolean", p.Kind)
}

func isRuntimePolicyNull(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
func runtimePredicateValue(p rowPolicyRuntimePredicate, context PolicyContext, record any) (any, error) {
	if p.Kind == "identity" {
		value, ok := context.identity(p.Name)
		if !ok {
			return nil, fmt.Errorf("missing identity %q", p.Name)
		}
		return value, nil
	}
	if p.Kind == "column" {
		rv := reflect.ValueOf(record)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			if rt.Field(i).Tag.Get("db") == p.Name {
				return rv.Field(i).Interface(), nil
			}
		}
		return nil, fmt.Errorf("record missing policy column %q", p.Name)
	}
	return nil, fmt.Errorf("predicate %q has no scalar value", p.Kind)
}
