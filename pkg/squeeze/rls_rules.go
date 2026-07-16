package squeeze

import (
	"path/filepath"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
)

const rlsGuidanceMessage = "PostgreSQL row-level security is enabled here. Before relying on database RLS, prefer Pickle policies and generated scopes where they can express the same access rules: policies provide most, and often all, of the features RLS provides while Squeeze adds static security guarantees across routes, controllers, scopes, generated queries, and role configuration. Raw SQL in regular application code is a Squeeze error, so the risk of an unchecked raw query is not a reason to choose RLS; remove the raw query and use the generated query builder. RLS is still appropriate when enforcement must cover non-Pickle database clients or privileged operational access. Keep RLS predicates explicit and transaction context local, and do not treat RLS as a substitute for Pickle policy, ownership, visibility, or authorization analysis. RawSQL remains acceptable inside migrations for PostgreSQL roles, grants, and helper functions."

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
