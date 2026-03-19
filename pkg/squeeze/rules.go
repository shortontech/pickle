package squeeze

import (
	"go/ast"
	"go/token"
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
		"no_printf":                ruleNoPrintf,
		"no_recover":               ruleNoRecover,
		"ownership_scoping":        ruleOwnershipScoping,
		"read_scoping":             ruleReadScoping,
		"enum_validation":          ruleEnumValidation,
		"uuid_error_handling":      ruleUUIDErrorHandling,
		"public_projection":        rulePublicProjection,
		"required_fields":          ruleRequiredFields,
		"unbounded_query":          ruleUnboundedQuery,
		"rate_limit_auth":          ruleRateLimitAuth,
		"auth_without_middleware":  ruleAuthWithoutMiddleware,
		"param_mismatch":           ruleParamMismatch,
		"csrf_missing":             ruleCsrfMissing,
		"sensitive_field_encryption":           ruleSensitiveFieldEncryption,
		"public_sensitive_conflict":             rulePublicSensitiveConflict,
		"immutable_raw_update":                  ruleImmutableRawUpdate,
		"immutable_raw_insert_missing_version":  ruleImmutableRawInsertMissingVersion,
		"immutable_timestamps_call":             ruleImmutableTimestampsCall,
		"immutable_direct_delete":               ruleImmutableDirectDelete,
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

// ruleNoRecover flags recover() calls in controllers and helpers.
// recover() silently swallows panics and hides bugs. Let panics crash loudly.
func ruleNoRecover(ctx *AnalysisContext) []Finding {
	var findings []Finding

	for _, m := range ctx.Methods {
		lines := FindBuiltinCalls(m.Body, m.Fset, "recover")
		for _, line := range lines {
			findings = append(findings, Finding{
				Rule:     "no_recover",
				Severity: SeverityError,
				File:     m.File,
				Line:     line,
				Message:  "recover() in controller — silently swallows panics and hides bugs, remove it",
			})
		}
	}

	for name, pf := range ctx.FuncRegistry {
		lines := FindBuiltinCalls(pf.Body, pf.Fset, "recover")
		for _, line := range lines {
			file := pf.Fset.Position(pf.Body.Pos()).Filename
			findings = append(findings, Finding{
				Rule:     "no_recover",
				Severity: SeverityError,
				File:     file,
				Line:     line,
				Message:  name + "() calls recover() — silently swallows panics and hides bugs, remove it",
			})
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

// sensitiveExactNames are field names that are inherently sensitive.
var sensitiveExactNames = map[string]bool{
	"password":      true,
	"email":         true,
	"ssn":           true,
	"access_token":  true,
	"api_key":       true,
	"session_key":   true,
	"refresh_token": true,
	"secret":        true,
	"private_key":   true,
	"credit_card":   true,
	"card_number":   true,
	"cvv":           true,
	"pin":           true,
	"date_of_birth": true,
	"phone":         true,
	"phone_number":  true,
}

// sensitiveSuffixes are column name suffixes that indicate sensitive data.
var sensitiveSuffixes = []string{
	"_secret",
	"_token",
	"_key",
	"_hash",
	"_password",
	"_ssn",
	"_credential",
}

// isSensitiveColumn returns true if the column name matches a known sensitive pattern.
func isSensitiveColumn(name string) bool {
	if sensitiveExactNames[name] {
		return true
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// ruleSensitiveFieldEncryption flags sensitive columns without .Encrypted().
func ruleSensitiveFieldEncryption(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, table := range ctx.Tables {
		for _, col := range table.Columns {
			if isSensitiveColumn(col.Name) && !col.IsEncrypted {
				findings = append(findings, Finding{
					Rule:     "sensitive_field_encryption",
					Severity: SeverityWarning,
					File:     "",
					Line:     0,
					Message:  table.Name + "." + col.Name + " — sensitive field without .Encrypted() (data at rest may be unprotected)",
				})
			}
		}
	}
	return findings
}

// rulePublicSensitiveConflict flags sensitive columns marked .Public() without .UnsafePublic().
func rulePublicSensitiveConflict(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, table := range ctx.Tables {
		for _, col := range table.Columns {
			if col.IsPublic && isSensitiveColumn(col.Name) && !col.IsUnsafePublic {
				findings = append(findings, Finding{
					Rule:     "public_sensitive_conflict",
					Severity: SeverityError,
					File:     "",
					Line:     0,
					Message:  table.Name + "." + col.Name + " — sensitive field marked .Public() (use .UnsafePublic() to override)",
				})
			}
		}
	}
	return findings
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

// ruleCsrfMissing flags state-changing routes without CSRF middleware when the project uses sessions.
// The session driver is always generated by Pickle, so its presence isn't a signal. Instead, we scan
// controllers and helper functions for calls to session.Create — that's the real indicator.
func ruleCsrfMissing(ctx *AnalysisContext) []Finding {
	if !projectUsesSessions(ctx) {
		return nil
	}

	var findings []Finding

	for _, route := range ctx.Routes {
		if route.Method == "GET" || route.Method == "HEAD" || route.Method == "OPTIONS" {
			continue
		}

		if route.HasCSRFMiddleware(ctx.Config.Middleware) {
			continue
		}

		findings = append(findings, Finding{
			Rule:     "csrf_missing",
			Severity: SeverityError,
			File:     route.File,
			Line:     route.Line,
			Message:  route.Method + " " + route.Path + " — missing CSRF middleware (cross-site request forgery vector)",
		})
	}

	return findings
}

// immutableTableNames returns a set of table names that are marked immutable.
func immutableTableNames(ctx *AnalysisContext) map[string]bool {
	names := map[string]bool{}
	for _, tbl := range ctx.Tables {
		if tbl.IsImmutable {
			names[tbl.Name] = true
		}
	}
	return names
}


// findRawSQLStrings walks an AST block and returns all string literal values
// along with their source line numbers.
func findRawSQLStrings(body *ast.BlockStmt, fset *token.FileSet) []struct {
	Value string
	Line  int
} {
	var results []struct {
		Value string
		Line  int
	}
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val := strings.Trim(lit.Value, "`\"")
		results = append(results, struct {
			Value string
			Line  int
		}{Value: val, Line: fset.Position(lit.Pos()).Line})
		return true
	})
	return results
}

// ruleImmutableRawUpdate flags raw UPDATE SQL statements targeting immutable tables.
func ruleImmutableRawUpdate(ctx *AnalysisContext) []Finding {
	immutable := immutableTableNames(ctx)
	if len(immutable) == 0 {
		return nil
	}
	var findings []Finding
	for _, m := range ctx.Methods {
		for _, s := range findRawSQLStrings(m.Body, m.Fset) {
			upper := strings.ToUpper(s.Value)
			for tbl := range immutable {
				if strings.Contains(upper, "UPDATE "+strings.ToUpper(tbl)) {
					findings = append(findings, Finding{
						Rule:     "immutable_raw_update",
						Severity: SeverityError,
						File:     m.File,
						Line:     s.Line,
						Message:  `raw UPDATE on immutable table "` + tbl + `" — call Query` + names.TableToStructName(tbl) + `().Update() instead, which inserts a new version`,
					})
				}
			}
		}
	}
	return findings
}

// ruleImmutableRawInsertMissingVersion flags raw INSERT statements into immutable
// tables that name explicit columns but omit version_id.
func ruleImmutableRawInsertMissingVersion(ctx *AnalysisContext) []Finding {
	immutable := immutableTableNames(ctx)
	if len(immutable) == 0 {
		return nil
	}
	var findings []Finding
	for _, m := range ctx.Methods {
		for _, s := range findRawSQLStrings(m.Body, m.Fset) {
			upper := strings.ToUpper(s.Value)
			for tbl := range immutable {
				if !strings.Contains(upper, "INSERT INTO "+strings.ToUpper(tbl)) {
					continue
				}
				// Only flag if the INSERT names explicit columns (has a "(") but omits version_id
				parenIdx := strings.Index(upper, "(")
				valuesIdx := strings.Index(upper, "VALUES")
				if parenIdx < 0 || (valuesIdx > 0 && parenIdx > valuesIdx) {
					continue
				}
				if !strings.Contains(upper, "VERSION_ID") {
					findings = append(findings, Finding{
						Rule:     "immutable_raw_insert_missing_version",
						Severity: SeverityError,
						File:     m.File,
						Line:     s.Line,
						Message:  `raw INSERT into immutable table "` + tbl + `" omits version_id — use Query` + names.TableToStructName(tbl) + `().Create() which generates a UUID v7 in Go`,
					})
				}
			}
		}
	}
	return findings
}

// ruleImmutableTimestampsCall flags t.Timestamps() called alongside t.Immutable()
// in migration files. Since squeeze currently parses controller code, this rule
// inspects the Tables slice for the post-generation anomaly (both IsImmutable and
// a created_at column present — which only happens if Timestamps() didn't panic).
// In practice the generator panics at build time; this rule adds a belt-and-suspenders
// check for any table that somehow has both.
func ruleImmutableTimestampsCall(ctx *AnalysisContext) []Finding {
	var findings []Finding
	for _, tbl := range ctx.Tables {
		if !tbl.IsImmutable {
			continue
		}
		for _, col := range tbl.Columns {
			if col.Name == "created_at" || col.Name == "updated_at" {
				findings = append(findings, Finding{
					Rule:     "immutable_timestamps_call",
					Severity: SeverityError,
					File:     "database/migrations",
					Line:     0,
					Message:  `immutable table "` + tbl.Name + `" has a ` + col.Name + ` column — Timestamps() must not be called on immutable tables; CreatedAt and UpdatedAt are derived from UUID v7 timestamps`,
				})
				break
			}
		}
	}
	return findings
}

// ruleImmutableDirectDelete flags raw DELETE SQL statements targeting immutable
// tables that have no SoftDeletes column.
func ruleImmutableDirectDelete(ctx *AnalysisContext) []Finding {
	// Build set of immutable tables without soft deletes
	hardImmutable := map[string]bool{}
	for _, tbl := range ctx.Tables {
		if tbl.IsImmutable && !tbl.HasSoftDelete {
			hardImmutable[tbl.Name] = true
		}
	}
	if len(hardImmutable) == 0 {
		return nil
	}
	var findings []Finding
	for _, m := range ctx.Methods {
		for _, s := range findRawSQLStrings(m.Body, m.Fset) {
			upper := strings.ToUpper(s.Value)
			for tbl := range hardImmutable {
				if strings.Contains(upper, "DELETE FROM "+strings.ToUpper(tbl)) {
					findings = append(findings, Finding{
						Rule:     "immutable_direct_delete",
						Severity: SeverityError,
						File:     m.File,
						Line:     s.Line,
						Message:  `raw DELETE on immutable table "` + tbl + `" — this table has no SoftDeletes(); only perform deliberate data erasure (e.g. GDPR) via raw SQL and document why`,
					})
				}
			}
		}
	}
	return findings
}

// projectUsesSessions returns true if any controller or helper function calls session.Create.
func projectUsesSessions(ctx *AnalysisContext) bool {
	// Check controller methods.
	for _, m := range ctx.Methods {
		if len(FindCallsTo(m.Body, m.Fset, "session", "Create")) > 0 {
			return true
		}
	}

	// Check helper functions in the registry (service layer, etc.).
	for _, pf := range ctx.FuncRegistry {
		if len(FindCallsTo(pf.Body, pf.Fset, "session", "Create")) > 0 {
			return true
		}
	}

	return false
}
