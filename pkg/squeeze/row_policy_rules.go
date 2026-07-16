package squeeze

import (
	"strconv"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
)

func ruleRowPolicyInvalid(ctx *AnalysisContext) []Finding {
	if ctx.RowPolicyError == "" {
		return nil
	}
	return []Finding{{Rule: "row_policy_invalid", Severity: SeverityError, File: "database/policies", Message: "row policy cannot be normalized safely: " + ctx.RowPolicyError}}
}

func ruleRowPolicyUnknownIdentity(ctx *AnalysisContext) []Finding {
	message := strings.ToLower(ctx.RowPolicyError)
	if !strings.Contains(message, "identity") || !(strings.Contains(message, "unknown") || strings.Contains(message, "type")) {
		return nil
	}
	return []Finding{{Rule: "row_policy_unknown_identity", Severity: SeverityError, File: "database/policies", Message: "row policy identity cannot be normalized safely: " + ctx.RowPolicyError}}
}

func ruleRowPolicyMissing(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, policy := range ctx.RowPolicies {
		for _, rule := range policy.Protection.Rules {
			if rule.Select == nil && rule.Insert == nil && rule.UpdateOld == nil && rule.UpdateNew == nil && rule.Delete == nil {
				findings = append(findings, Finding{Rule: "row_policy_missing", Severity: SeverityError, File: sourceRowPolicyFile(policy), Message: "row policy rule " + rule.Key + " for " + policy.Protection.Table + " has no protected operation"})
			}
			if (rule.UpdateOld == nil) != (rule.UpdateNew == nil) {
				findings = append(findings, Finding{Rule: "row_policy_missing", Severity: SeverityError, File: sourceRowPolicyFile(policy), Message: "row policy rule " + rule.Key + " for " + policy.Protection.Table + " must define both existing and proposed update predicates"})
			}
		}
	}
	return findings
}

func ruleRowPolicyUnlowerable(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, policy := range ctx.RowPolicies {
		if policy.EnforcementClass != "portable" {
			continue
		}
		if _, err := generator.LowerPostgresRowPolicies([]generator.ResolvedRowPolicy{policy}); err != nil {
			findings = append(findings, Finding{Rule: "row_policy_unlowerable", Severity: SeverityError, File: sourceRowPolicyFile(policy), Message: "portable row policy for " + policy.Protection.Table + " does not lower equivalently: " + err.Error()})
		}
	}
	return findings
}

func ruleRowPolicyBypass(ctx *AnalysisContext) []Finding {
	if len(ctx.RowPolicies) == 0 {
		return nil
	}
	base := append(ruleRawSQL(ctx), ruleRawQueryBuilderAccess(ctx)...)
	findings := make([]Finding, 0, len(base))
	for _, finding := range base {
		finding.Rule = "row_policy_bypass"
		finding.Severity = SeverityError
		finding.Message = "protected query path can bypass generated row-policy enforcement: " + finding.Message
		findings = append(findings, finding)
	}
	return findings
}

// Projection conflicts require a proven row/column exposure mismatch. The
// normalized metadata deliberately emits no finding when that proof is absent.
func ruleRowPolicyProjectionConflict(*AnalysisContext) []Finding { return nil }

// Live-only RLS findings are produced by rls:status, which has the database
// catalog and role inspection needed to prove these conditions. Registering
// the rule IDs keeps static Squeeze configuration and proof output stable
// without guessing from source files.
func ruleRLSNotEnabled(*AnalysisContext) []Finding    { return nil }
func ruleRLSNotForced(*AnalysisContext) []Finding     { return nil }
func ruleRLSRuntimeBypass(*AnalysisContext) []Finding { return nil }
func ruleRLSDrift(*AnalysisContext) []Finding         { return nil }

func ruleRowPolicyApplicationOnly(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, policy := range ctx.RowPolicies {
		if policy.EnforcementClass == "application_only" {
			findings = append(findings, Finding{Rule: "row_policy_application_only", Severity: SeverityWarning, File: sourceRowPolicyFile(policy), Message: "row policy for " + policy.Protection.Table + " is enforced by generated application queries but cannot be lowered equivalently to PostgreSQL RLS because its logical operation shares a physical SQL command; the explicit application-only acknowledgement prevents a false dual-enforcement claim"})
		}
	}
	return findings
}

func ruleRowPolicyContextMissing(ctx *AnalysisContext) []Finding {
	protected := map[string]generator.ResolvedRowPolicy{}
	for _, policy := range ctx.RowPolicies {
		protected[policy.Protection.Table] = policy
	}
	if len(protected) == 0 {
		return nil
	}
	var findings []Finding
	for name, method := range ctx.Methods {
		chains := ExtractCallChainsRecursive(method.Body, method.Fset, ctx.FuncRegistry, nil)
		type access struct {
			query string
			table string
			line  int
			safe  bool
		}
		accesses := map[string]access{}
		for _, chain := range chains {
			names := chain.Names()
			queryName := ""
			for _, segment := range names {
				if strings.HasPrefix(segment, "Query") && len(segment) > 5 {
					queryName = strings.TrimPrefix(segment, "Query")
					break
				}
			}
			if queryName == "" {
				continue
			}
			table := rowPolicyTableForQuery(protected, queryName)
			if table == "" {
				continue
			}
			safe := len(names) > 0 && strings.EqualFold(names[0], "tx")
			for _, segment := range names {
				if segment == "WithPolicyContext" {
					safe = true
				}
			}
			key := table + "\x00" + queryName + "\x00" + method.File + "\x00" + strconv.Itoa(chain.Line)
			current, exists := accesses[key]
			if !exists || safe && !current.safe {
				accesses[key] = access{query: queryName, table: table, line: chain.Line, safe: safe}
			}
		}
		for _, access := range accesses {
			if !access.safe {
				findings = append(findings, Finding{Rule: "row_policy_context_missing", Severity: SeverityError, File: method.File, Line: access.line, Message: name + " reaches protected " + access.table + " through " + access.query + " without an explicit policy context or policy-scoped transaction"})
			}
		}
	}
	return findings
}

func ruleRowPolicyContextSpoof(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for name, method := range ctx.Methods {
		for _, line := range FindCallsTo(method.Body, method.Fset, "models", "NewVerifiedPolicyContext") {
			findings = append(findings, Finding{Rule: "row_policy_context_spoof", Severity: SeverityError, File: method.File, Line: line, Message: name + " constructs verified policy identity directly — only generated authentication, job, CLI, or test adapters may create PolicyContext"})
		}
	}
	for name, fn := range ctx.FuncRegistry {
		for _, line := range FindCallsTo(fn.Body, fn.Fset, "models", "NewVerifiedPolicyContext") {
			findings = append(findings, Finding{Rule: "row_policy_context_spoof", Severity: SeverityError, File: fn.Fset.Position(fn.Body.Pos()).Filename, Line: line, Message: name + "() constructs verified policy identity directly — use a generated trusted adapter"})
		}
	}
	return findings
}

func rowPolicyTableForQuery(policies map[string]generator.ResolvedRowPolicy, query string) string {
	normalized := strings.ToLower(strings.ReplaceAll(query, "_", ""))
	for table := range policies {
		candidate := strings.ToLower(strings.ReplaceAll(singularTableName(table), "_", ""))
		if candidate == normalized {
			return table
		}
	}
	return ""
}
func singularTableName(table string) string {
	parts := strings.Split(table, ".")
	name := parts[len(parts)-1]
	if strings.HasSuffix(name, "ies") {
		return strings.TrimSuffix(name, "ies") + "y"
	}
	if strings.HasSuffix(name, "s") {
		return strings.TrimSuffix(name, "s")
	}
	return name
}
func sourceRowPolicyFile(policy generator.ResolvedRowPolicy) string {
	if len(policy.SourcePolicies) == 0 {
		return "database/policies"
	}
	return "database/policies/" + policy.SourcePolicies[len(policy.SourcePolicies)-1] + ".go"
}
