package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

func ruleSeederMissingValue(ctx *AnalysisContext) []Finding {
	tables := map[string]*schema.Table{}
	for _, table := range ctx.Tables {
		tables[table.Name] = table
	}
	overrides := map[string]bool{}
	for _, definition := range ctx.Seeders {
		for _, call := range definition.GraphCalls {
			if (call.Method == "With" || call.Method == "WithFactory") && len(call.Arguments) > 0 {
				if name, err := strconv.Unquote(call.Arguments[0]); err == nil {
					overrides[name] = true
				}
			}
		}
	}
	var findings []Finding
	for _, definition := range ctx.Seeders {
		if definition.Kind != "row" || len(definition.Fields) == 0 {
			continue
		}
		table := tables[definition.Table]
		if table == nil {
			continue
		}
		provided := map[string]bool{}
		for _, field := range definition.Fields {
			provided[field.Name] = true
		}
		for _, column := range table.Columns {
			managed := table.IsImmutable || table.IsAppendOnly
			if provided[column.Name] || overrides[column.Name] || column.Seeder != nil && column.Seeder.Kind != "none" || column.HasDefault || column.IsNullable || column.ForeignKeyTable != "" || column.IsPrimaryKey || managed && (column.Name == "row_hash" || column.Name == "prev_hash" || column.Name == "version_id") {
				continue
			}
			findings = append(findings, Finding{Rule: "seeder_missing_value", Severity: SeverityError, File: definition.File, Line: 1, Message: "typed row seeder " + definition.Name + " leaves required column " + table.Name + "." + column.Name + " without a resolvable value source"})
		}
	}
	return findings
}

func ruleSeederTypeMismatch(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, definition := range ctx.Seeders {
		if definition.Kind != "row" {
			continue
		}
		if err := generator.ValidateSeederDefinitions([]generator.SeederDefinition{definition}, ctx.Tables); err != nil {
			findings = append(findings, Finding{Rule: "seeder_type_mismatch", Severity: SeverityError, File: definition.File, Line: 1, Message: err.Error()})
		}
	}
	return findings
}

func ruleSeederAmbiguousRelationship(ctx *AnalysisContext) []Finding {
	rows := map[string]generator.SeederDefinition{}
	for _, definition := range ctx.Seeders {
		if definition.Kind == "row" {
			rows[definition.Name] = definition
		}
	}
	tables := map[string]*schema.Table{}
	for _, table := range ctx.Tables {
		tables[table.Name] = table
	}
	var findings []Finding
	for _, scenario := range ctx.Seeders {
		if scenario.Kind != "scenario" {
			continue
		}
		created := map[string]bool{}
		forCount, forLine := 0, 1
		explicit := false
		for _, call := range scenario.GraphCalls {
			switch call.Method {
			case "Create", "CreateN", "DependsOn":
				if len(call.Arguments) > 0 {
					name := strings.TrimSuffix(strings.TrimSpace(call.Arguments[0]), "Ref")
					if definition, ok := rows[name]; ok {
						created[definition.Table] = true
					}
				}
			case "For":
				forCount++
				forLine = call.Line
				explicit = explicit || len(call.Arguments) > 1
			}
		}
		if forCount == 0 || explicit || len(created) != 2 {
			continue
		}
		for childName := range created {
			child := tables[childName]
			if child == nil {
				continue
			}
			for parentName := range created {
				if parentName == childName {
					continue
				}
				matches := 0
				for _, fk := range child.ForeignKeys {
					if fk.ReferencedTable == parentName {
						matches++
					}
				}
				for _, column := range child.Columns {
					if column.ForeignKeyTable == parentName {
						matches++
					}
				}
				if matches > 1 {
					findings = append(findings, Finding{Rule: "seeder_ambiguous_relationship", Severity: SeverityError, File: scenario.File, Line: forLine, Message: "For has multiple relationships from " + childName + " to " + parentName + "; select one with Through"})
				}
			}
		}
	}
	return findings
}

func ruleSeederIncompleteCompositeKey(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, scenario := range ctx.Seeders {
		if scenario.Kind != "scenario" {
			continue
		}
		with := map[string]int{}
		for _, call := range scenario.GraphCalls {
			if call.Method != "With" || len(call.Arguments) == 0 {
				continue
			}
			if name, err := strconv.Unquote(call.Arguments[0]); err == nil {
				with[name] = call.Line
			}
		}
		for _, table := range ctx.Tables {
			for _, fk := range table.ForeignKeys {
				if len(fk.Columns) < 2 {
					continue
				}
				matched, line := 0, 1
				for _, column := range fk.Columns {
					if at := with[column]; at > 0 {
						matched++
						line = at
					}
				}
				if matched > 0 && matched < len(fk.Columns) {
					findings = append(findings, Finding{Rule: "seeder_incomplete_composite_key", Severity: SeverityError, File: scenario.File, Line: line, Message: "scenario overrides only part of composite relationship " + table.Name + "(" + strings.Join(fk.Columns, ", ") + ")"})
				}
			}
		}
	}
	return findings
}

func ruleSeederSensitiveLiteral(ctx *AnalysisContext) []Finding {
	seen := map[string]bool{}
	var findings []Finding
	for _, definition := range ctx.Seeders {
		if seen[definition.File] {
			continue
		}
		seen[definition.File] = true
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, definition.File, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "With" && len(call.Args) > 1 && sensitiveSeedKey(call.Args[0]) && isStringSeedLiteral(call.Args[1]) {
					findings = append(findings, Finding{Rule: "seeder_sensitive_literal", Severity: SeverityError, File: definition.File, Line: fset.Position(call.Pos()).Line, Message: "sensitive seed column contains a literal; use deterministic fixture derivation or an explicit safe provider"})
				}
			}
			pair, ok := node.(*ast.KeyValueExpr)
			if ok && sensitiveSeedKey(pair.Key) && isStringSeedLiteral(pair.Value) {
				findings = append(findings, Finding{Rule: "seeder_sensitive_literal", Severity: SeverityError, File: definition.File, Line: fset.Position(pair.Pos()).Line, Message: "sensitive seed field contains a literal; use deterministic fixture derivation or an explicit safe provider"})
			}
			return true
		})
	}
	return findings
}

func sensitiveSeedKey(expression ast.Expr) bool {
	name := ""
	switch value := expression.(type) {
	case *ast.BasicLit:
		name, _ = strconv.Unquote(value.Value)
	case *ast.Ident:
		name = value.Name
	}
	name = strings.ToLower(name)
	for _, marker := range []string{"password", "secret", "token", "credential", "api_key", "private_key"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func isStringSeedLiteral(expression ast.Expr) bool {
	literal, ok := expression.(*ast.BasicLit)
	return ok && literal.Kind == token.STRING
}

// Production mutation cannot currently be configured without the mandatory
// force and exact-environment confirmation gates, so there is no unsafe state
// for static analysis to report.
func ruleSeederProductionUnsafe(*AnalysisContext) []Finding { return nil }

func ruleSeederIntegrityOverride(ctx *AnalysisContext) []Finding {
	seen := map[string]bool{}
	var findings []Finding
	for _, definition := range ctx.Seeders {
		if seen[definition.File] {
			continue
		}
		seen[definition.File] = true
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, definition.File, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.BasicLit:
				if value.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(value.Value)
				if err != nil || text != "row_hash" && text != "prev_hash" {
					return true
				}
				findings = append(findings, Finding{Rule: "seeder_integrity_override", Severity: SeverityError, File: definition.File, Line: fset.Position(value.Pos()).Line, Message: text + " is framework-derived for immutable and append-only seed rows; remove the authored value"})
			case *ast.KeyValueExpr:
				if ident, ok := value.Key.(*ast.Ident); ok && (ident.Name == "RowHash" || ident.Name == "PrevHash") {
					findings = append(findings, Finding{Rule: "seeder_integrity_override", Severity: SeverityError, File: definition.File, Line: fset.Position(value.Pos()).Line, Message: ident.Name + " is framework-derived for immutable and append-only seed rows; remove the authored value"})
				}
			}
			return true
		})
	}
	return findings
}

func ruleSeederUnstableIdentity(ctx *AnalysisContext) []Finding {
	rowTables := map[string]string{}
	for _, definition := range ctx.Seeders {
		if definition.Kind == "row" {
			rowTables[definition.Name] = definition.Table
		}
	}
	tableByName := map[string]*schema.Table{}
	for _, table := range ctx.Tables {
		tableByName[table.Name] = table
	}
	var findings []Finding
	for _, definition := range ctx.Seeders {
		if definition.Kind != "scenario" {
			continue
		}
		policy := definition.Policy
		if index := strings.LastIndex(policy, "."); index >= 0 {
			policy = policy[index+1:]
		}
		if policy != "InsertOrIgnore" && policy != "Upsert" {
			continue
		}
		creates, totalCreates, identities, updates, line := 0, 0, 0, 0, 1
		for _, call := range definition.GraphCalls {
			switch call.Method {
			case "Create", "CreateN":
				totalCreates++
				hasAuthoritativePrimary := false
				if len(call.Arguments) > 0 {
					name := strings.TrimSuffix(strings.TrimSpace(call.Arguments[0]), "Ref")
					if table := tableByName[rowTables[name]]; table != nil {
						hasAuthoritativePrimary = len(seedRulePrimaryColumns(table)) > 0
					}
				}
				if !hasAuthoritativePrimary {
					creates++
				}
				if line == 1 {
					line = call.Line
				}
			case "UniqueBy":
				identities++
			case "Update":
				updates++
			}
		}
		if identities < creates {
			findings = append(findings, Finding{Rule: "seeder_unstable_identity", Severity: SeverityError, File: definition.File, Line: line, Message: "repeat policy " + policy + " requires UniqueBy on every created row node; Pickle will not guess conflict identities"})
			continue
		}
		if policy == "Upsert" && updates < totalCreates {
			findings = append(findings, Finding{Rule: "seeder_unstable_identity", Severity: SeverityError, File: definition.File, Line: line, Message: "Upsert requires an explicit Update allowlist on every created row node"})
		}
	}
	return findings
}

func seedRulePrimaryColumns(table *schema.Table) []string {
	if len(table.CompositePrimaryKeys) > 0 {
		return table.CompositePrimaryKeys
	}
	var columns []string
	for _, column := range table.Columns {
		if column.IsPrimaryKey {
			columns = append(columns, column.Name)
		}
	}
	return columns
}

func ruleSeederNondeterministic(ctx *AnalysisContext) []Finding {
	seen := map[string]bool{}
	var findings []Finding
	for _, definition := range ctx.Seeders {
		if seen[definition.File] {
			continue
		}
		seen[definition.File] = true
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, definition.File, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			nondeterministic := pkg.Name == "time" && selector.Sel.Name == "Now"
			if pkg.Name == "rand" && selector.Sel.Name != "New" && selector.Sel.Name != "NewSource" {
				nondeterministic = true
			}
			if nondeterministic {
				findings = append(findings, Finding{Rule: "seeder_nondeterministic", Severity: SeverityError, File: definition.File, Line: fset.Position(call.Pos()).Line, Message: pkg.Name + "." + selector.Sel.Name + " bypasses the deterministic seed context"})
			}
			return true
		})
	}
	return findings
}
