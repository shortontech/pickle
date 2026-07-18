package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

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
		creates, identities, updates, line := 0, 0, 0, 1
		for _, call := range definition.GraphCalls {
			switch call.Method {
			case "Create", "CreateN":
				creates++
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
		if policy == "Upsert" && updates < creates {
			findings = append(findings, Finding{Rule: "seeder_unstable_identity", Severity: SeverityError, File: definition.File, Line: line, Message: "Upsert requires an explicit Update allowlist on every created row node"})
		}
	}
	return findings
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
