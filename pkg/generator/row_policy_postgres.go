package generator

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/shortontech/pickle/pkg/schema"
)

type GeneratedPostgresRowPolicy struct {
	Name, Table, Using, WithCheck string
	Command                       schema.RLSPolicyCommand
	RuleKeys                      []string
}
type PostgresRowPolicyPlan struct {
	Table         string
	Enable, Force bool
	Policies      []GeneratedPostgresRowPolicy
}

func LowerPostgresRowPolicies(policies []ResolvedRowPolicy) ([]PostgresRowPolicyPlan, error) {
	var plans []PostgresRowPolicyPlan
	for _, resolved := range policies {
		if resolved.EnforcementClass != "portable" {
			continue
		}
		plan := PostgresRowPolicyPlan{Table: resolved.Protection.Table, Enable: true, Force: true}
		for _, operation := range []string{"select", "insert", "update", "delete"} {
			if resolved.PhysicalPlans[operation] == "" || strings.Contains(resolved.PhysicalPlans[operation], "application_only") {
				continue
			}
			policy, ok, err := lowerPostgresOperation(resolved, operation)
			if err != nil {
				return nil, fmt.Errorf("lowering %s.%s: %w", resolved.Protection.Table, operation, err)
			}
			if ok {
				plan.Policies = append(plan.Policies, policy)
			}
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func lowerPostgresOperation(resolved ResolvedRowPolicy, operation string) (GeneratedPostgresRowPolicy, bool, error) {
	var usingParts, checkParts, keys []string
	for _, rule := range resolved.Protection.Rules {
		subject, err := postgresSubjectPredicate(rule.Subject)
		if err != nil {
			return GeneratedPostgresRowPolicy{}, false, err
		}
		var using, check *schema.RowPredicate
		switch operation {
		case "select":
			using = rule.Select
		case "insert":
			check = rule.Insert
		case "update":
			using, check = rule.UpdateOld, rule.UpdateNew
		case "delete":
			using = rule.Delete
		}
		if using == nil && check == nil {
			continue
		}
		keys = append(keys, rule.Key)
		if using != nil {
			body, err := postgresRowPredicate(*using, resolved.Identities)
			if err != nil {
				return GeneratedPostgresRowPolicy{}, false, err
			}
			usingParts = append(usingParts, "("+subject+" AND "+body+")")
		}
		if check != nil {
			body, err := postgresRowPredicate(*check, resolved.Identities)
			if err != nil {
				return GeneratedPostgresRowPolicy{}, false, err
			}
			checkParts = append(checkParts, "("+subject+" AND "+body+")")
		}
	}
	if len(keys) == 0 {
		return GeneratedPostgresRowPolicy{}, false, nil
	}
	join := " OR "
	if resolved.Protection.SubjectCombination == schema.AllOfSubjects {
		join = " AND "
	}
	command := map[string]schema.RLSPolicyCommand{"select": schema.RLSSelect, "insert": schema.RLSInsert, "update": schema.RLSUpdate, "delete": schema.RLSDelete}[operation]
	return GeneratedPostgresRowPolicy{Name: generatedRowPolicyName(resolved.Protection.Table, operation), Table: resolved.Protection.Table, Command: command, Using: joinPredicates(usingParts, join), WithCheck: joinPredicates(checkParts, join), RuleKeys: keys}, true, nil
}

func postgresSubjectPredicate(subject schema.RowSubject) (string, error) {
	switch subject.Kind {
	case schema.SubjectPublic:
		return "TRUE", nil
	case schema.SubjectAuthenticated:
		return `pickle_identity_present('user_id')`, nil
	case schema.SubjectRole:
		return "pickle_identity_has_role(" + quotePolicyLiteral(subject.Name) + ")", nil
	default:
		return "", fmt.Errorf("unknown subject %q", subject.Kind)
	}
}

func postgresRowPredicate(predicate schema.RowPredicate, identities map[string]schema.PolicyIdentityType) (string, error) {
	switch predicate.Kind {
	case schema.PredicateAllow:
		return "TRUE", nil
	case schema.PredicateDeny:
		return "FALSE", nil
	case schema.PredicateColumn:
		return quotePolicyIdent(predicate.Name), nil
	case schema.PredicateIdentity:
		kind, ok := identities[predicate.Name]
		if !ok {
			return "", fmt.Errorf("unknown identity %q", predicate.Name)
		}
		fn := "pickle_identity_text"
		if kind == schema.PolicyIdentityUUID {
			fn = "pickle_identity_uuid"
		}
		if kind == schema.PolicyIdentityStrings {
			return "", fmt.Errorf("identity set %q cannot be scalar", predicate.Name)
		}
		return fn + "(" + quotePolicyLiteral(predicate.Name) + ")", nil
	case schema.PredicateEqual, schema.PredicateNotEqual:
		if len(predicate.Children) != 2 {
			return "", fmt.Errorf("comparison requires two children")
		}
		left, err := postgresRowPredicate(predicate.Children[0], identities)
		if err != nil {
			return "", err
		}
		right, err := postgresRowPredicate(predicate.Children[1], identities)
		if err != nil {
			return "", err
		}
		op := "="
		if predicate.Kind == schema.PredicateNotEqual {
			op = "<>"
		}
		return "COALESCE((" + left + " " + op + " " + right + "), FALSE)", nil
	case schema.PredicateAnd, schema.PredicateOr:
		parts := make([]string, len(predicate.Children))
		for i, child := range predicate.Children {
			part, err := postgresRowPredicate(child, identities)
			if err != nil {
				return "", err
			}
			parts[i] = part
		}
		join := " AND "
		if predicate.Kind == schema.PredicateOr {
			join = " OR "
		}
		return "(" + strings.Join(parts, join) + ")", nil
	case schema.PredicateNot:
		if len(predicate.Children) != 1 {
			return "", fmt.Errorf("not requires one child")
		}
		child, err := postgresRowPredicate(predicate.Children[0], identities)
		if err != nil {
			return "", err
		}
		return "COALESCE(NOT (" + child + "), FALSE)", nil
	default:
		return "", fmt.Errorf("unsupported predicate %q", predicate.Kind)
	}
}

func generatedRowPolicyName(table, operation string) string {
	base := "pickle_" + strings.ReplaceAll(table, ".", "_") + "_" + operation
	sum := fmt.Sprintf("%x", sha256.Sum256([]byte(table+":"+operation)))[:10]
	max := 63 - 1 - len(sum)
	if len(base) > max {
		base = base[:max]
	}
	return base + "_" + sum
}
func joinPredicates(parts []string, join string) string {
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, join) + ")"
}
func quotePolicyLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func quotePolicyIdent(value string) string {
	parts := strings.Split(value, ".")
	for i, part := range parts {
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func PostgresPolicyIdentityHelpers() []string {
	return []string{
		`CREATE OR REPLACE FUNCTION pickle_identity_present(identity_name text) RETURNS boolean LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('pickle.identity.' || identity_name, true), '') IS NOT NULL $$`,
		`CREATE OR REPLACE FUNCTION pickle_identity_text(identity_name text) RETURNS text LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('pickle.identity.' || identity_name, true), '') $$`,
		`CREATE OR REPLACE FUNCTION pickle_identity_uuid(identity_name text) RETURNS uuid LANGUAGE plpgsql STABLE AS $$ DECLARE raw text; BEGIN raw := NULLIF(current_setting('pickle.identity.' || identity_name, true), ''); IF raw IS NULL THEN RETURN NULL; END IF; BEGIN RETURN raw::uuid; EXCEPTION WHEN invalid_text_representation THEN RETURN NULL; END; END $$`,
		`CREATE OR REPLACE FUNCTION pickle_identity_has_role(role_name text) RETURNS boolean LANGUAGE plpgsql STABLE AS $$ DECLARE raw text; BEGIN raw := NULLIF(current_setting('pickle.identity.roles', true), ''); IF raw IS NULL OR length(raw) > 65536 THEN RETURN FALSE; END IF; BEGIN RETURN raw::jsonb ? role_name; EXCEPTION WHEN invalid_text_representation THEN RETURN FALSE; END; END $$`,
	}
}
