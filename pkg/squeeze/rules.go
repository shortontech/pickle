package squeeze

import (
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/names"
	"github.com/shortontech/pickle/pkg/schema"
)

// AnalysisContext holds all parsed project data for rules to inspect.
type AnalysisContext struct {
	Routes     []AnalyzedRoute
	Methods    map[string]*ControllerMethod
	Requests   []generator.RequestDef
	Tables     []*schema.Table
	Config     SqueezeConfig
}

// Rule is a function that inspects the analysis context and returns findings.
type Rule func(ctx *AnalysisContext) []Finding

// AllRules returns all available rules keyed by name.
func AllRules() map[string]Rule {
	return map[string]Rule{
		"no_printf":           ruleNoPrintf,
		"ownership_scoping":   ruleOwnershipScoping,
		"enum_validation":     ruleEnumValidation,
		"uuid_error_handling": ruleUUIDErrorHandling,
		"public_projection":   rulePublicProjection,
		"required_fields":     ruleRequiredFields,
	}
}

// ruleNoPrintf flags fmt.Printf/Sprintf/Println/Print/Fprintf in controllers.
func ruleNoPrintf(ctx *AnalysisContext) []Finding {
	var findings []Finding
	fmtFuncs := []string{"Printf", "Sprintf", "Println", "Print", "Fprintf", "Errorf"}

	for _, m := range ctx.Methods {
		for _, fn := range fmtFuncs {
			lines := FindCallsTo(m.Body, m.Fset, "fmt", fn)
			for _, line := range lines {
				findings = append(findings, Finding{
					Rule:     "no_printf",
					Severity: SeverityWarning,
					File:     m.File,
					Line:     line,
					Message:  "fmt." + fn + " in controller — use structured logging instead",
				})
			}
		}
	}

	return findings
}

// ruleOwnershipScoping flags DELETE/UPDATE routes that don't scope queries by the authenticated user.
func ruleOwnershipScoping(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		if route.Method != "DELETE" && route.Method != "PUT" && route.Method != "PATCH" {
			continue
		}

		// Admin routes are exempt
		if route.HasAdminMiddleware(ctx.Config.Middleware) {
			continue
		}

		// Must have auth middleware to check ownership
		if !route.HasAuthMiddleware(ctx.Config.Middleware) {
			continue
		}

		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		chains := ExtractCallChains(method.Body, method.Fset)

		// Look for query chains that include a Where* with ctx.Auth() in args
		hasOwnershipScope := false
		for _, chain := range chains {
			chainNames := chain.Names()
			// Must be a model query chain (starts with models or has Query in it)
			isQueryChain := false
			for _, name := range chainNames {
				if strings.HasPrefix(name, "Query") {
					isQueryChain = true
					break
				}
			}
			if !isQueryChain {
				continue
			}

			if chain.HasSegmentWithAuthArg("Where") {
				hasOwnershipScope = true
				break
			}
		}

		if !hasOwnershipScope {
			findings = append(findings, Finding{
				Rule:     "ownership_scoping",
				Severity: SeverityError,
				File:     method.File,
				Line:     method.Line,
				Message:  route.Method + " " + route.Path + " — query does not scope by authenticated user (missing Where* with ctx.Auth())",
			})
		}
	}

	return findings
}

// enumFields are field name patterns that should have oneof validation.
var enumFields = map[string]bool{
	"status":   true,
	"role":     true,
	"type":     true,
	"state":    true,
	"kind":     true,
	"category": true,
}

// ruleEnumValidation flags request struct fields named status/role/type/state without oneof= validation.
func ruleEnumValidation(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, req := range ctx.Requests {
		for _, field := range req.Fields {
			fieldLower := strings.ToLower(field.Name)
			if !enumFields[fieldLower] {
				continue
			}
			if strings.Contains(field.Validate, "oneof=") {
				continue
			}
			findings = append(findings, Finding{
				Rule:     "enum_validation",
				Severity: SeverityError,
				File:     "", // we don't have file info from ScanRequests — filled in by orchestrator
				Line:     0,
				Message:  req.Name + "." + field.Name + " — state/role field missing oneof= validation (allows arbitrary values like \"god_mode\")",
			})
		}
	}

	return findings
}

// ruleUUIDErrorHandling flags uuid.MustParse(ctx.Param(...)) calls.
func ruleUUIDErrorHandling(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, m := range ctx.Methods {
		calls := FindMustParseCalls(m.Body, m.Fset)
		for _, call := range calls {
			if call.HasCtxParam {
				findings = append(findings, Finding{
					Rule:     "uuid_error_handling",
					Severity: SeverityError,
					File:     m.File,
					Line:     call.Line,
					Message:  "uuid.MustParse(ctx.Param(...)) — panics on invalid input, use uuid.Parse with error handling",
				})
			} else if call.HasCtxAuth {
				findings = append(findings, Finding{
					Rule:     "uuid_error_handling",
					Severity: SeverityWarning,
					File:     m.File,
					Line:     call.Line,
					Message:  "uuid.MustParse(ctx.Auth()...) — consider uuid.Parse for defense in depth",
				})
			}
		}
	}

	return findings
}

// rulePublicProjection flags unauthenticated routes that return model data without .Public().
func rulePublicProjection(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		// Only check routes without auth middleware
		if route.HasAuthMiddleware(ctx.Config.Middleware) {
			continue
		}

		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		jsonCalls := FindCtxJSONCalls(method.Body, method.Fset)
		for _, jc := range jsonCalls {
			if PayloadIsModelWithoutPublic(jc.PayloadExpr) {
				findings = append(findings, Finding{
					Rule:     "public_projection",
					Severity: SeverityError,
					File:     method.File,
					Line:     jc.Line,
					Message:  route.Method + " " + route.Path + " — unauthenticated route returns data without .Public() projection",
				})
			}
		}
	}

	return findings
}

// ruleRequiredFields flags Create() calls where the model struct literal is missing required fields.
func ruleRequiredFields(ctx *AnalysisContext) []Finding {
	var findings []Finding

	// Build a map of table name -> required fields (not nullable, no default, not PK)
	requiredByTable := make(map[string][]string)
	for _, table := range ctx.Tables {
		var required []string
		for _, col := range table.Columns {
			if col.IsPrimaryKey || col.IsNullable || col.HasDefault || col.DefaultValue != nil {
				continue
			}
			// Skip timestamp columns (created_at, updated_at) — typically auto-set
			if col.Name == "created_at" || col.Name == "updated_at" {
				continue
			}
			required = append(required, col.Name)
		}
		if len(required) > 0 {
			requiredByTable[table.Name] = required
		}
	}

	for _, m := range ctx.Methods {
		// Find composite literals in the method
		lits := FindCompositeLiterals(m.Body, m.Fset)
		for _, lit := range lits {
			if lit.PackageName != "models" {
				continue
			}

			// Map model type name to table name
			// Post -> posts (simple pluralization)
			tableName := strings.ToLower(lit.TypeName) + "s"
			required, ok := requiredByTable[tableName]
			if !ok {
				continue
			}

			// Check if there's a Create call nearby using this literal
			chains := ExtractCallChains(m.Body, m.Fset)
			hasCreate := false
			for _, chain := range chains {
				for _, name := range chain.Names() {
					if name == "Create" {
						hasCreate = true
						break
					}
				}
			}
			if !hasCreate {
				continue
			}

			// Check which required fields are set
			setFields := make(map[string]bool)
			for _, f := range lit.FieldNames {
				setFields[f] = true
			}

			for _, reqCol := range required {
				goField := names.SnakeToPascal(reqCol)
				if !setFields[goField] {
					findings = append(findings, Finding{
						Rule:     "required_fields",
						Severity: SeverityError,
						File:     m.File,
						Line:     lit.Line,
						Message:  lit.TypeName + "{} missing required field " + goField + " (column " + reqCol + " is NOT NULL with no default)",
					})
				}
			}
		}
	}

	return findings
}
