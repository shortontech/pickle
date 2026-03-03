package squeeze

import (
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/names"
	"github.com/shortontech/pickle/pkg/schema"
)

// AnalysisContext holds all parsed project data for rules to inspect.
type AnalysisContext struct {
	Routes       []AnalyzedRoute
	Methods      map[string]*ControllerMethod
	Requests     []generator.RequestDef
	Tables       []*schema.Table
	Config       SqueezeConfig
	FuncRegistry FuncRegistry
}

// Rule is a function that inspects the analysis context and returns findings.
type Rule func(ctx *AnalysisContext) []Finding

// AllRules returns all available rules keyed by name.
func AllRules() map[string]Rule {
	return map[string]Rule{
		"no_printf":           ruleNoPrintf,
		"ownership_scoping":   ruleOwnershipScoping,
		"read_scoping":        ruleReadScoping,
		"enum_validation":     ruleEnumValidation,
		"uuid_error_handling": ruleUUIDErrorHandling,
		"public_projection":   rulePublicProjection,
		"required_fields":     ruleRequiredFields,
		"unbounded_query":     ruleUnboundedQuery,
		"rate_limit_auth":     ruleRateLimitAuth,
		"auth_without_middleware": ruleAuthWithoutMiddleware,
		"param_mismatch":          ruleParamMismatch,
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

		authVars := FindAuthTaintedVars(method.Body)
		chains := ExtractCallChainsRecursive(method.Body, method.Fset, ctx.FuncRegistry, authVars)

		// Look for query chains that include a Where* with ctx.Auth() in args
		// (either directly or via a local variable derived from ctx.Auth())
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

			if chain.HasSegmentWithAuthArgTainted("Where", authVars) {
				hasOwnershipScope = true
				break
			}
			// AnyOwner() is an explicit opt-out of ownership scoping
			if chain.HasSegment("AnyOwner") {
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
				File:     req.File,
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

		modelVars := FindModelVars(method.Body)
		jsonCalls := FindCtxJSONCalls(method.Body, method.Fset)
		for _, jc := range jsonCalls {
			if PayloadIsModelWithoutPublic(jc.PayloadExpr, modelVars) {
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
		// Find composite literals in the method (and recursively in called functions)
		lits := FindCompositeLiteralsRecursive(m.Body, m.Fset, ctx.FuncRegistry)
		for _, lit := range lits {
			if lit.PackageName != "models" {
				continue
			}

			// Map model type name to table name
			// Post -> posts, Category -> categories
			tableName := names.Pluralize(lit.TypeName)
			required, ok := requiredByTable[tableName]
			if !ok {
				continue
			}

			// Check if there's a Create call on the specific model's query builder
			// e.g. models.QueryPost().Create(&post) — chain contains ["models", "QueryPost", "Create"]
			authVarsReq := FindAuthTaintedVars(m.Body)
			chains := ExtractCallChainsRecursive(m.Body, m.Fset, ctx.FuncRegistry, authVarsReq)
			expectedQuery := "Query" + lit.TypeName
			hasCreate := false
			for _, chain := range chains {
				chainNames := chain.Names()
				for i, name := range chainNames {
					if name == "Create" && i > 0 && chainNames[i-1] == expectedQuery {
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

// ruleReadScoping flags GET routes behind auth that query models without scoping by the authenticated user.
func ruleReadScoping(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		if route.Method != "GET" {
			continue
		}

		// Admin routes are exempt
		if route.HasAdminMiddleware(ctx.Config.Middleware) {
			continue
		}

		// Must have auth middleware — unauth reads are a different concern
		if !route.HasAuthMiddleware(ctx.Config.Middleware) {
			continue
		}

		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		authVars := FindAuthTaintedVars(method.Body)
		chains := ExtractCallChainsRecursive(method.Body, method.Fset, ctx.FuncRegistry, authVars)

		hasOwnershipScope := false
		for _, chain := range chains {
			chainNames := chain.Names()
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

			if chain.HasSegmentWithAuthArgTainted("Where", authVars) {
				hasOwnershipScope = true
				break
			}
			// AnyOwner() is an explicit opt-out of ownership scoping
			if chain.HasSegment("AnyOwner") {
				hasOwnershipScope = true
				break
			}
		}

		if !hasOwnershipScope {
			findings = append(findings, Finding{
				Rule:     "read_scoping",
				Severity: SeverityError,
				File:     method.File,
				Line:     method.Line,
				Message:  route.Method + " " + route.Path + " — authenticated read does not scope by user (possible IDOR)",
			})
		}
	}

	return findings
}

// ruleUnboundedQuery flags routes that call .All() without .Limit().
// Unauthenticated routes are errors (DoS vector). Authenticated routes are warnings (resource waste).
func ruleUnboundedQuery(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		authVarsUnbounded := FindAuthTaintedVars(method.Body)
		chains := ExtractCallChainsRecursive(method.Body, method.Fset, ctx.FuncRegistry, authVarsUnbounded)

		for _, chain := range chains {
			chainNames := chain.Names()

			isQueryChain := false
			hasAll := false
			hasLimit := false
			for _, name := range chainNames {
				if strings.HasPrefix(name, "Query") {
					isQueryChain = true
				}
				if name == "All" {
					hasAll = true
				}
				if name == "Limit" || name == "Paginate" {
					hasLimit = true
				}
			}

			if isQueryChain && hasAll && !hasLimit {
				findings = append(findings, Finding{
					Rule:     "unbounded_query",
					Severity: SeverityError,
					File:     method.File,
					Line:     method.Line,
					Message:  route.Method + " " + route.Path + " — .All() without .Limit() (unbounded response size)",
				})
			}
		}
	}

	return findings
}

// authMethodNames are controller method names that handle credential-based authentication.
var authMethodNames = map[string]bool{
	"Login":    true,
	"Register": true,
	"Store":    true, // on AuthController — registration
}

// authPathSegments are URL path segments that indicate an auth endpoint.
var authPathSegments = []string{"/login", "/register", "/signup", "/auth"}

// isAuthRoute returns true if the route looks like an authentication endpoint,
// based on controller name, method name, or path pattern.
func isAuthRoute(route AnalyzedRoute) bool {
	// Controller name contains "Auth"
	if strings.Contains(route.ControllerType, "Auth") && authMethodNames[route.MethodName] {
		return true
	}
	// Path contains auth-related segments
	pathLower := strings.ToLower(route.Path)
	for _, seg := range authPathSegments {
		if strings.HasSuffix(pathLower, seg) || strings.Contains(pathLower, seg+"/") {
			return true
		}
	}
	return false
}

// ruleRateLimitAuth flags authentication routes (login, register) that lack rate limiting middleware.
func ruleRateLimitAuth(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		if route.Method != "POST" {
			continue
		}

		if !isAuthRoute(route) {
			continue
		}

		if route.HasRateLimitMiddleware(ctx.Config.Middleware) {
			continue
		}

		findings = append(findings, Finding{
			Rule:     "rate_limit_auth",
			Severity: SeverityError,
			File:     route.File,
			Line:     route.Line,
			Message:  route.Method + " " + route.Path + " — auth endpoint without rate limiting (brute force vector)",
		})
	}

	return findings
}

// ruleParamMismatch flags ctx.Param() calls where the param name doesn't match any route parameter.
func ruleParamMismatch(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		routeParams := make(map[string]bool)
		for _, p := range RouteParams(route.Path) {
			routeParams[p] = true
		}

		paramCalls := FindParamNames(method.Body, method.Fset)
		for _, pc := range paramCalls {
			if !routeParams[pc.Name] {
				findings = append(findings, Finding{
					Rule:     "param_mismatch",
					Severity: SeverityError,
					File:     method.File,
					Line:     pc.Line,
					Message:  route.Method + " " + route.Path + " — ctx.Param(\"" + pc.Name + "\") does not match any route parameter (will panic)",
				})
			}
		}
	}

	return findings
}

// ruleAuthWithoutMiddleware flags controllers on unauthenticated routes that call ctx.Auth().
// Without auth middleware, ctx.Auth() panics. This is always a bug.
func ruleAuthWithoutMiddleware(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, route := range ctx.Routes {
		if route.HasAuthMiddleware(ctx.Config.Middleware) {
			continue
		}

		key := route.ControllerType + "." + route.MethodName
		method, ok := ctx.Methods[key]
		if !ok {
			continue
		}

		if bodyContainsAuthCall(method.Body) {
			findings = append(findings, Finding{
				Rule:     "auth_without_middleware",
				Severity: SeverityError,
				File:     method.File,
				Line:     method.Line,
				Message:  route.Method + " " + route.Path + " — calls ctx.Auth() without auth middleware (will panic)",
			})
		}
	}

	return findings
}
