package picklemcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

type rowPolicyInput struct {
	Table     string `json:"table"`
	Operation string `json:"operation,omitempty"`
	Subject   string `json:"subject,omitempty"`
}

func (s *Server) policiesRows(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)
	if state.RowPolicyError != "" {
		return errResult("row policies are invalid: " + state.RowPolicyError), nil, nil
	}
	if len(state.RowPolicies) == 0 {
		return textResult("No Pickle row policies defined."), nil, nil
	}
	var b strings.Builder
	for _, policy := range state.RowPolicies {
		fmt.Fprintf(&b, "## %s\nClassification: %s\n", policy.Protection.Table, rowPolicyClassification(policy))
		fmt.Fprintf(&b, "Required identities: %s\n", strings.Join(sortedIdentityNames(policy.Identities), ", "))
		fmt.Fprintf(&b, "Operations: %s\nRule IDs: %s\nSources: %s\n\n", strings.Join(rowPolicyOperations(policy), ", "), strings.Join(rowPolicyRuleKeys(policy), ", "), strings.Join(policy.SourcePolicies, ", "))
	}
	return textResult(b.String()), nil, nil
}

func (s *Server) policiesRow(_ context.Context, _ *mcp.CallToolRequest, input rowPolicyInput) (*mcp.CallToolResult, any, error) {
	policy, result := s.findRowPolicy(input.Table)
	if result != nil {
		return result, nil, nil
	}
	return textResult(renderRowPolicy(policy)), nil, nil
}

func (s *Server) policiesExplain(_ context.Context, _ *mcp.CallToolRequest, input rowPolicyInput) (*mcp.CallToolResult, any, error) {
	policy, result := s.findRowPolicy(input.Table)
	if result != nil {
		return result, nil, nil
	}
	operation := strings.ToLower(strings.TrimSpace(input.Operation))
	if operation == "" {
		return errResult("operation is required (select, insert, update, or delete)"), nil, nil
	}
	var matching []schema.RowRule
	for _, rule := range policy.Protection.Rules {
		if input.Subject != "" && input.Subject != rowPolicySubject(rule.Subject) && input.Subject != rule.Subject.Name {
			continue
		}
		if rowRuleHasOperation(rule, operation) {
			matching = append(matching, rule)
		}
	}
	if len(matching) == 0 {
		return errResult("no matching normalized rule for that table, operation, and subject"), nil, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Table: %s\nOperation: %s\nClassification: %s\n", policy.Protection.Table, operation, rowPolicyClassification(policy))
	fmt.Fprintf(&b, "Application query shape: generated terminal operation adds the normalized %s predicate before database access\n", operation)
	fmt.Fprintf(&b, "Required context: %s\n", strings.Join(sortedIdentityNames(policy.Identities), ", "))
	for _, rule := range matching {
		fmt.Fprintf(&b, "Rule %s (%s): %s\n", rule.Key, rowPolicySubject(rule.Subject), renderRuleOperation(rule, operation))
	}
	plans, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{policy})
	if err != nil {
		fmt.Fprintf(&b, "PostgreSQL RLS: unlowerable (%v)\n", err)
	} else if len(plans) == 0 {
		b.WriteString("PostgreSQL RLS: not emitted; application-only acknowledgement applies\n")
	} else {
		var names []string
		for _, generated := range plans[0].Policies {
			if strings.EqualFold(string(generated.Command), operation) {
				names = append(names, generated.Name)
			}
		}
		fmt.Fprintf(&b, "PostgreSQL RLS: equivalent generated policy %s; RLS enabled and forced\n", strings.Join(names, ", "))
	}
	fmt.Fprintf(&b, "Source policies: %s\n", strings.Join(policy.SourcePolicies, ", "))
	return textResult(b.String()), nil, nil
}

func (s *Server) rlsStatus(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	state := DeriveRBACState(s.project.Dir)
	if state.RowPolicyError != "" {
		return errResult("row policies are invalid: " + state.RowPolicyError), nil, nil
	}
	plans, err := generator.LowerPostgresRowPolicies(state.RowPolicies)
	if err != nil {
		return errResult("RLS lowering failed: " + err.Error()), nil, nil
	}
	if len(plans) == 0 {
		return textResult("No portable generated PostgreSQL RLS state. Live catalog was not inspected."), nil, nil
	}
	var b strings.Builder
	b.WriteString("Desired generated PostgreSQL RLS state (live catalog not inspected):\n")
	for _, plan := range plans {
		fmt.Fprintf(&b, "- %s: enabled=%t forced=%t fingerprint=%s\n", plan.Table, plan.Enable, plan.Force, generator.GeneratedRowPolicyFingerprint([]generator.PostgresRowPolicyPlan{plan}))
		for _, policy := range plan.Policies {
			fmt.Fprintf(&b, "  - %s FOR %s rules=%s\n", policy.Name, policy.Command, strings.Join(policy.RuleKeys, ","))
		}
	}
	b.WriteString("Use `pickle rls:status` for explicit read-only live catalog comparison.\n")
	return textResult(b.String()), nil, nil
}

func (s *Server) findRowPolicy(table string) (generator.ResolvedRowPolicy, *mcp.CallToolResult) {
	if strings.TrimSpace(table) == "" {
		return generator.ResolvedRowPolicy{}, errResult("table is required")
	}
	state := DeriveRBACState(s.project.Dir)
	if state.RowPolicyError != "" {
		return generator.ResolvedRowPolicy{}, errResult("row policies are invalid: " + state.RowPolicyError)
	}
	for _, policy := range state.RowPolicies {
		if policy.Protection.Table == table {
			return policy, nil
		}
	}
	return generator.ResolvedRowPolicy{}, errResult("no row policy protects table " + table)
}

func renderRowPolicy(policy generator.ResolvedRowPolicy) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Table: %s\nClassification: %s\nSubject combination: %s\n", policy.Protection.Table, rowPolicyClassification(policy), policy.Protection.SubjectCombination)
	for _, name := range sortedIdentityNames(policy.Identities) {
		fmt.Fprintf(&b, "Identity: %s (%s)\n", name, policy.Identities[name])
	}
	for _, rule := range policy.Protection.Rules {
		fmt.Fprintf(&b, "Rule %s subject=%s", rule.Key, rowPolicySubject(rule.Subject))
		for _, operation := range []string{"select", "insert", "update", "delete"} {
			if rowRuleHasOperation(rule, operation) {
				fmt.Fprintf(&b, " %s={%s}", operation, renderRuleOperation(rule, operation))
			}
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Sources: %s\n", strings.Join(policy.SourcePolicies, ", "))
	return b.String()
}

func rowPolicyClassification(policy generator.ResolvedRowPolicy) string {
	if policy.EnforcementClass == "portable" {
		return "unproven (portable application enforcement and generated RLS; live catalog and entry-point reachability not inspected)"
	}
	return policy.EnforcementClass
}
func sortedIdentityNames(identities map[string]schema.PolicyIdentityType) []string {
	names := make([]string, 0, len(identities))
	for name := range identities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func rowPolicyRuleKeys(policy generator.ResolvedRowPolicy) []string {
	keys := make([]string, 0, len(policy.Protection.Rules))
	for _, rule := range policy.Protection.Rules {
		keys = append(keys, rule.Key)
	}
	return keys
}
func rowPolicyOperations(policy generator.ResolvedRowPolicy) []string {
	var operations []string
	for _, operation := range []string{"select", "insert", "update", "delete"} {
		for _, rule := range policy.Protection.Rules {
			if rowRuleHasOperation(rule, operation) {
				operations = append(operations, operation)
				break
			}
		}
	}
	return operations
}
func rowPolicySubject(subject schema.RowSubject) string {
	if subject.Name == "" {
		return string(subject.Kind)
	}
	return string(subject.Kind) + ":" + subject.Name
}
func rowRuleHasOperation(rule schema.RowRule, operation string) bool {
	switch operation {
	case "select":
		return rule.Select != nil
	case "insert":
		return rule.Insert != nil
	case "update":
		return rule.UpdateOld != nil && rule.UpdateNew != nil
	case "delete":
		return rule.Delete != nil
	}
	return false
}
func renderRuleOperation(rule schema.RowRule, operation string) string {
	switch operation {
	case "select":
		return renderRowPredicate(rule.Select)
	case "insert":
		return renderRowPredicate(rule.Insert)
	case "update":
		return "existing " + renderRowPredicate(rule.UpdateOld) + "; proposed " + renderRowPredicate(rule.UpdateNew)
	case "delete":
		return renderRowPredicate(rule.Delete)
	}
	return "unknown"
}
func renderRowPredicate(predicate *schema.RowPredicate) string {
	if predicate == nil {
		return "none"
	}
	switch predicate.Kind {
	case schema.PredicateAllow, schema.PredicateDeny:
		return string(predicate.Kind)
	case schema.PredicateColumn:
		return "column(" + predicate.Name + ")"
	case schema.PredicateIdentity:
		return "identity(" + predicate.Name + ")"
	case schema.PredicateNot:
		return "not(" + renderRowPredicate(&predicate.Children[0]) + ")"
	default:
		parts := make([]string, len(predicate.Children))
		for i := range predicate.Children {
			parts[i] = renderRowPredicate(&predicate.Children[i])
		}
		return string(predicate.Kind) + "(" + strings.Join(parts, ", ") + ")"
	}
}
