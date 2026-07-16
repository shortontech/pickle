package schema

import (
	"fmt"
	"strings"
)

// SQLPredicate is an explicitly raw PostgreSQL row-level-security expression.
// It is only used while applying migrations and is never interpolated at runtime.
type SQLPredicate string

type RLSPolicyCommand string

const (
	RLSAll    RLSPolicyCommand = "ALL"
	RLSSelect RLSPolicyCommand = "SELECT"
	RLSInsert RLSPolicyCommand = "INSERT"
	RLSUpdate RLSPolicyCommand = "UPDATE"
	RLSDelete RLSPolicyCommand = "DELETE"
)

// RLSPolicy describes a PostgreSQL CREATE POLICY statement.
type RLSPolicy struct {
	Name      string
	Table     string
	Command   RLSPolicyCommand
	Roles     []string
	Using     SQLPredicate
	WithCheck SQLPredicate
}

func (p *RLSPolicy) For(command RLSPolicyCommand) *RLSPolicy { p.Command = command; return p }
func (p *RLSPolicy) To(roles ...string) *RLSPolicy {
	p.Roles = append([]string(nil), roles...)
	return p
}
func (p *RLSPolicy) UsingExpression(predicate SQLPredicate) *RLSPolicy { p.Using = predicate; return p }
func (p *RLSPolicy) WithCheckExpression(predicate SQLPredicate) *RLSPolicy {
	p.WithCheck = predicate
	return p
}
func (p *RLSPolicy) WithSameCheck() *RLSPolicy { p.WithCheck = p.Using; return p }

func validateRLSIdentifier(kind, value string) {
	if strings.TrimSpace(value) == "" || strings.ContainsRune(value, '\x00') {
		panic("pickle: RLS " + kind + " must not be empty or contain NUL")
	}
}

func validateRLSPolicy(p *RLSPolicy) {
	validateRLSIdentifier("policy name", p.Name)
	validateRLSIdentifier("table name", p.Table)
	if p.Command == "" {
		p.Command = RLSAll
	}
	switch p.Command {
	case RLSAll, RLSSelect, RLSInsert, RLSUpdate, RLSDelete:
	default:
		panic("pickle: invalid RLS policy command \"" + string(p.Command) + "\"")
	}
	for _, role := range p.Roles {
		validateRLSIdentifier("role", role)
	}
	if strings.ContainsRune(string(p.Using), '\x00') || strings.ContainsRune(string(p.WithCheck), '\x00') {
		panic("pickle: RLS predicates must not contain NUL")
	}
	if p.Using == "" && p.WithCheck == "" {
		panic("pickle: RLS policy requires a USING or WITH CHECK expression")
	}
	if p.Command == RLSSelect || p.Command == RLSDelete {
		if p.WithCheck != "" {
			panic("pickle: SELECT and DELETE RLS policies cannot have WITH CHECK")
		}
	}
	if p.Command == RLSInsert && p.Using != "" {
		panic("pickle: INSERT RLS policies cannot have USING")
	}
}

func (m *Migration) EnableRLS(table string)  { m.rlsTableOp(OpEnableRLS, table) }
func (m *Migration) DisableRLS(table string) { m.rlsTableOp(OpDisableRLS, table) }
func (m *Migration) ForceRLS(table string)   { m.rlsTableOp(OpForceRLS, table) }
func (m *Migration) NoForceRLS(table string) { m.rlsTableOp(OpNoForceRLS, table) }
func (m *Migration) rlsTableOp(kind TableOperation, table string) {
	validateRLSIdentifier("table name", table)
	m.Operations = append(m.Operations, Operation{Type: kind, Table: table})
}

func (m *Migration) CreateRLSPolicy(table, name string, configure func(*RLSPolicy)) {
	p := &RLSPolicy{Name: name, Table: table, Command: RLSAll}
	if configure != nil {
		configure(p)
	}
	validateRLSPolicy(p)
	m.Operations = append(m.Operations, Operation{Type: OpCreateRLSPolicy, Table: table, RLSPolicy: p})
}

func (m *Migration) DropRLSPolicy(table, name string) {
	validateRLSIdentifier("table name", table)
	validateRLSIdentifier("policy name", name)
	m.Operations = append(m.Operations, Operation{Type: OpDropRLSPolicy, Table: table, RLSPolicy: &RLSPolicy{Name: name, Table: table}})
}

func quoteRLSIdent(value string) string {
	parts := strings.Split(value, ".")
	for i, part := range parts {
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func postgresRLSSQL(op Operation) (string, error) {
	table := quoteRLSIdent(op.Table)
	switch op.Type {
	case OpEnableRLS:
		return "ALTER TABLE " + table + " ENABLE ROW LEVEL SECURITY", nil
	case OpDisableRLS:
		return "ALTER TABLE " + table + " DISABLE ROW LEVEL SECURITY", nil
	case OpForceRLS:
		return "ALTER TABLE " + table + " FORCE ROW LEVEL SECURITY", nil
	case OpNoForceRLS:
		return "ALTER TABLE " + table + " NO FORCE ROW LEVEL SECURITY", nil
	case OpDropRLSPolicy:
		if op.RLSPolicy == nil {
			return "", fmt.Errorf("RLS policy definition missing")
		}
		return "DROP POLICY IF EXISTS " + quoteRLSIdent(op.RLSPolicy.Name) + " ON " + table, nil
	case OpCreateRLSPolicy:
		if op.RLSPolicy == nil {
			return "", fmt.Errorf("RLS policy definition missing")
		}
		p := op.RLSPolicy
		q := "CREATE POLICY " + quoteRLSIdent(p.Name) + " ON " + table + " FOR " + string(p.Command)
		if len(p.Roles) > 0 {
			roles := make([]string, len(p.Roles))
			for i, role := range p.Roles {
				if strings.EqualFold(role, "PUBLIC") {
					roles[i] = "PUBLIC"
				} else {
					roles[i] = quoteRLSIdent(role)
				}
			}
			q += " TO " + strings.Join(roles, ", ")
		}
		if p.Using != "" {
			q += " USING (" + string(p.Using) + ")"
		}
		if p.WithCheck != "" {
			q += " WITH CHECK (" + string(p.WithCheck) + ")"
		}
		return q, nil
	default:
		return "", fmt.Errorf("not an RLS operation")
	}
}
