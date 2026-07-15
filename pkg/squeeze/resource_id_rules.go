package squeeze

import (
	"go/ast"
	"strconv"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/names"
	"github.com/shortontech/pickle/pkg/schema"
)

// ResourceIDOrigins describes values that static analysis has positively
// identified as ResourceIDs. It deliberately contains no name-only guesses.
type ResourceIDOrigins struct {
	Vars       map[string]bool
	PartsVars  map[string]bool
	RequestVar map[string]map[string]bool
	Params     map[string]bool
}

// FindResourceIDOrigins finds ResourceIDs introduced by ParamResourceID,
// ParamResourceIDParts, or generated request binders for typed request fields.
func FindResourceIDOrigins(body *ast.BlockStmt, requests []generator.RequestDef) ResourceIDOrigins {
	origins := ResourceIDOrigins{Vars: map[string]bool{}, PartsVars: map[string]bool{}, RequestVar: map[string]map[string]bool{}, Params: map[string]bool{}}
	requestFields := map[string]map[string]bool{}
	for _, request := range requests {
		fields := map[string]bool{}
		for _, field := range request.Fields {
			if field.IsResourceID {
				fields[field.Name] = true
			}
		}
		requestFields[request.Name] = fields
	}

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if ok {
			if name := resourceIDParamCall(call); name != "" {
				origins.Params[name] = true
			}
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) == 0 {
			return true
		}
		for _, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}
			if resourceIDParamCall(call) != "" {
				markAssignedIdentifiers(assign.Lhs, origins.Vars)
				if calledMethod(call) == "ParamResourceIDParts" {
					markAssignedIdentifiers(assign.Lhs, origins.PartsVars)
				}
				continue
			}
			if requestName := boundRequestName(call); requestName != "" {
				for _, lhs := range assign.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" && ident.Name != "err" && len(requestFields[requestName]) > 0 {
						origins.RequestVar[ident.Name] = requestFields[requestName]
						break
					}
				}
			}
		}
		return true
	})

	// Propagate only through assignments whose RHS is already proven.
	for {
		changed := false
		ast.Inspect(body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range assign.Rhs {
				if exprContainsResourceID(rhs, origins) {
					for _, lhs := range assign.Lhs {
						if ident, ok := lhs.(*ast.Ident); ok && ident.Name != "_" && ident.Name != "err" && !origins.Vars[ident.Name] {
							origins.Vars[ident.Name] = true
							changed = true
						}
					}
				}
			}
			return true
		})
		if !changed {
			break
		}
	}
	return origins
}

func calledMethod(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

func markAssignedIdentifiers(lhs []ast.Expr, vars map[string]bool) {
	for _, expr := range lhs {
		if ident, ok := expr.(*ast.Ident); ok && ident.Name != "_" && ident.Name != "err" {
			vars[ident.Name] = true
			break // the second result is normally error
		}
	}
}

func resourceIDParamCall(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "ParamResourceID" && sel.Sel.Name != "ParamResourceIDParts") || len(call.Args) != 1 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return ""
	}
	name, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return name
}

func boundRequestName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(sel.Sel.Name, "Bind") || !strings.HasSuffix(sel.Sel.Name, "Request") {
		return ""
	}
	return strings.TrimPrefix(sel.Sel.Name, "Bind")
}

func exprContainsResourceID(expr ast.Expr, origins ResourceIDOrigins) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		switch value := n.(type) {
		case *ast.Ident:
			found = origins.Vars[value.Name]
		case *ast.SelectorExpr:
			if base, ok := value.X.(*ast.Ident); ok {
				found = origins.RequestVar[base.Name][value.Sel.Name]
			}
		}
		return !found
	})
	return found
}

// ruleResourceIDUUIDParser flags UUID-only parsers applied to values proven to
// be ResourceIDs. Ordinary UUID parameters and unrelated id names are ignored.
func ruleResourceIDUUIDParser(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, method := range ctx.Methods {
		origins := FindResourceIDOrigins(method.Body, ctx.Requests)
		ast.Inspect(method.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			bad := false
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "uuid" && (sel.Sel.Name == "Parse" || sel.Sel.Name == "MustParse") {
				for _, arg := range call.Args {
					bad = bad || exprContainsResourceID(arg, origins)
				}
			}
			if sel.Sel.Name == "ParamUUID" && len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok {
					if name, err := strconv.Unquote(lit.Value); err == nil {
						bad = origins.Params[name]
					}
				}
			}
			if bad {
				findings = append(findings, Finding{
					Rule: "resource_id_uuid_parser", Severity: SeverityError,
					File: method.File, Line: method.Fset.Position(call.Pos()).Line,
					Message: "ResourceID passed to UUID parser; use ParseResourceID, ParamResourceID, or the already-decoded value",
				})
			}
			return true
		})
	}
	return findings
}

// ruleResourceIDUnscoped fires only for the exact flow Pickle can prove: a
// ResourceIDParts.RecordID is used against the local half of a two-column
// composite primary key, while the matching ScopeID is absent from the query.
func ruleResourceIDUnscoped(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, method := range ctx.Methods {
		origins := FindResourceIDOrigins(method.Body, ctx.Requests)
		if len(origins.PartsVars) == 0 {
			continue
		}
		for _, chain := range ExtractCallChains(method.Body, method.Fset) {
			if !chain.HasSegment("First") && !chain.HasSegment("All") {
				continue
			}
			for _, table := range ctx.Tables {
				keys := compositePrimaryKeyColumns(table)
				if len(keys) != 2 || !chain.HasSegment("Query"+names.TableToStructName(table.Name)) {
					continue
				}
				recordWhere := "Where" + names.SnakeToPascal(keys[1])
				scopeWhere := "Where" + names.SnakeToPascal(keys[0])
				for partsVar := range origins.PartsVars {
					if chainSegmentHasSelectorArg(chain, recordWhere, partsVar, "RecordID") && !chainSegmentHasSelectorArg(chain, scopeWhere, partsVar, "ScopeID") {
						findings = append(findings, Finding{
							Rule: "resource_id_unscoped", Severity: SeverityError, File: method.File, Line: chain.Line,
							Message: table.Name + " query uses decoded RecordID without the matching ScopeID; a valid ResourceID is not authorization",
						})
					}
				}
			}
		}
	}
	return findings
}

func compositePrimaryKeyColumns(table *schema.Table) []string {
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

func chainSegmentHasSelectorArg(chain CallChain, segmentName, variable, field string) bool {
	for _, segment := range chain.Segments {
		if segment.Name != segmentName {
			continue
		}
		for _, arg := range segment.Args {
			sel, ok := arg.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != field {
				continue
			}
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == variable {
				return true
			}
		}
	}
	return false
}
