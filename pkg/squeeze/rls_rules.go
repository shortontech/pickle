package squeeze

import (
	"path/filepath"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
)

const rlsGuidanceMessage = "Manual PostgreSQL row-level security is configured here. Define compatible row authorization once as a Pickle row policy: Pickle enforces it in generated application queries and emits equivalent PostgreSQL RLS. Do not duplicate portable authorization as hand-written RLS. Raw SQL in regular application code is a Squeeze error, not justification for a second policy system. Migration SQL remains appropriate for roles, grants, helper functions, and explicitly registered restrictive defense-in-depth policies."

func isRLSOperation(op generator.MigrationOperation) bool {
	if op.Type == "enable_rls" || op.Type == "force_rls" || op.Type == "create_rls_policy" {
		return true
	}
	if op.Type != "raw_sql" {
		return false
	}
	sql := strings.ToUpper(op.SQL)
	return strings.Contains(sql, "ROW LEVEL SECURITY") || strings.Contains(sql, "CREATE POLICY")
}

// ruleRLSGuidance gives agents architectural context when a project opts into
// database RLS. It is deliberately advisory: RLS is supported, but it should
// complement rather than replace Pickle's statically analyzed policy layer.
func ruleRLSGuidance(ctx *AnalysisContext) []Finding {
	for _, migration := range ctx.Migrations {
		usesRLS := false
		for _, op := range migration.Up {
			if isRLSOperation(op) {
				usesRLS = true
				break
			}
		}
		if !usesRLS {
			continue
		}
		file := ""
		if migration.File != "" {
			file = filepath.Join("database", "migrations", migration.File)
		}
		return []Finding{{
			Rule:     "rls_guidance",
			Severity: SeverityWarning,
			File:     file,
			Message:  rlsGuidanceMessage,
		}}
	}
	return nil
}

// ruleRLSManualBroadening rejects manual permissive policy state beside a
// Pickle-managed table. PostgreSQL ORs permissive policies, so even a policy
// that looks independently sensible can widen the generated predicate.
func ruleRLSManualBroadening(ctx *AnalysisContext) []Finding {
	protected := map[string]bool{}
	for _, policy := range ctx.RowPolicies {
		protected[policy.Protection.Table] = true
	}
	if len(protected) == 0 {
		return nil
	}
	var findings []Finding
	for _, migration := range ctx.Migrations {
		file := filepath.Join("database", "migrations", migration.File)
		for _, op := range migration.Up {
			table := strings.Trim(op.Table, `"`)
			if protected[table] && (op.Type == "disable_rls" || op.Type == "no_force_rls") {
				findings = append(findings, Finding{Rule: "rls_manual_broadening", Severity: SeverityError, File: file, Message: "migration weakens generated RLS on protected table " + table})
			}
			if op.Type == "create_rls_policy" && protected[table] {
				if op.RLSPolicy == nil || !op.RLSPolicy.Restrictive {
					findings = append(findings, Finding{Rule: "rls_manual_broadening", Severity: SeverityError, File: file, Message: "manual permissive policy on protected table " + table + " can broaden generated RLS; remove it or mark a genuinely narrowing policy RestrictiveDefenseInDepth()"})
				}
				if op.RLSPolicy != nil && strings.HasPrefix(strings.ToLower(op.RLSPolicy.Name), "pickle_") {
					findings = append(findings, Finding{Rule: "rls_manual_broadening", Severity: SeverityError, File: file, Message: "manual policy uses the reserved pickle_ generated namespace"})
				}
			}
			if op.Type != "raw_sql" {
				continue
			}
			upper := strings.ToUpper(op.SQL)
			if strings.Contains(upper, "CREATE POLICY") || strings.Contains(upper, "ALTER POLICY") || strings.Contains(upper, "DISABLE ROW LEVEL SECURITY") || strings.Contains(upper, "NO FORCE ROW LEVEL SECURITY") || strings.Contains(upper, "BYPASSRLS") {
				findings = append(findings, Finding{Rule: "rls_manual_broadening", Severity: SeverityError, File: file, Message: "raw policy-affecting SQL cannot be proven compatible with generated row policy; use the structured restrictive RLS DSL or separate operational role migration"})
			}
		}
	}
	return findings
}
