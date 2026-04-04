package squeeze

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shortontech/pickle/pkg/schema"
)

var reControllerActionCall = regexp.MustCompile(`ControllerAction\("([^"]+)",\s*([^)]+)\)`)

// ruleGraphQLUnexposedMutation flags controller actions registered as routes
// but not exposed in any GraphQL policy. Warning severity — REST-only is fine,
// but the developer should make a conscious choice.
func ruleGraphQLUnexposedMutation(ctx *AnalysisContext) []Finding {
	if ctx.GraphQLExposed == nil {
		return nil // no GraphQL policies → rule doesn't apply
	}

	var findings []Finding
	for _, route := range ctx.Routes {
		if route.Method != "POST" && route.Method != "PUT" && route.Method != "PATCH" && route.Method != "DELETE" {
			continue
		}
		// Extract controller.Method from handler
		handler := route.ControllerType + "." + route.MethodName
		if route.ControllerType == "" || route.MethodName == "" {
			continue
		}
		// Check if this action is exposed in GraphQL policies
		// Actions are registered by mutation name, which typically matches the method name
		// We flag routes whose controller methods could be GraphQL mutations but aren't
		parts := strings.Split(handler, ".")
		if len(parts) < 2 {
			continue
		}
		methodName := parts[len(parts)-1]
		if methodName == "" {
			continue
		}

		// Check if this method name appears as a ControllerAction in policies
		actionExposed := false
		policiesDir := filepath.Join(ctx.ProjectDir, "database", "policies", "graphql")
		if entries, err := os.ReadDir(policiesDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_gen.go") {
					continue
				}
				data, _ := os.ReadFile(filepath.Join(policiesDir, e.Name()))
				if strings.Contains(string(data), "ControllerAction(") && strings.Contains(string(data), methodName) {
					actionExposed = true
					break
				}
			}
		}

		if !actionExposed {
			findings = append(findings, Finding{
				Rule:     "graphql_unexposed_mutation",
				Severity: SeverityWarning,
				File:     route.File,
				Line:     route.Line,
				Message:  "route " + route.Method + " " + route.Path + " (" + handler + ") is not exposed as a GraphQL mutation — add ControllerAction() in a GraphQL policy or confirm REST-only",
			})
		}
	}
	return findings
}

// ruleGraphQLExposedNoMigration flags GraphQL policies that expose a model
// with no corresponding migration table. The model doesn't exist.
func ruleGraphQLExposedNoMigration(ctx *AnalysisContext) []Finding {
	if ctx.GraphQLExposed == nil {
		return nil
	}

	tableNames := map[string]bool{}
	for _, tbl := range ctx.Tables {
		tableNames[tbl.Name] = true
	}

	var findings []Finding
	policiesDir := filepath.Join(ctx.ProjectDir, "database", "policies", "graphql")
	entries, err := os.ReadDir(policiesDir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_gen.go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(policiesDir, e.Name()))
		if err != nil {
			continue
		}
		src := string(data)
		for _, match := range reExpose.FindAllStringSubmatch(src, -1) {
			model := match[1]
			if !tableNames[model] {
				findings = append(findings, Finding{
					Rule:     "graphql_exposed_no_migration",
					Severity: SeverityError,
					File:     filepath.Join(policiesDir, e.Name()),
					Line:     lineNumber(src, match[0]),
					Message:  "GraphQL policy exposes model \"" + model + "\" but no migration defines a table with that name",
				})
			}
		}
	}
	return findings
}

// ruleGraphQLActionNoController flags ControllerAction references to methods
// that don't exist in any parsed controller.
func ruleGraphQLActionNoController(ctx *AnalysisContext) []Finding {
	if ctx.GraphQLExposed == nil {
		return nil
	}

	var findings []Finding
	policiesDir := filepath.Join(ctx.ProjectDir, "database", "policies", "graphql")
	entries, err := os.ReadDir(policiesDir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_gen.go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(policiesDir, e.Name()))
		if err != nil {
			continue
		}
		src := string(data)

		// Match ControllerAction("name", controllers.XxxController{}.Method)
		for _, match := range reControllerActionCall.FindAllStringSubmatch(src, -1) {
			actionName := match[1]
			controllerRef := match[2]

			// Parse "controllers.UserController{}.Ban" -> "UserController.Ban"
			methodKey := parseControllerRef(controllerRef)
			if methodKey == "" {
				continue
			}

			// Check if the method exists in parsed controllers
			if _, ok := ctx.Methods[methodKey]; !ok {
				findings = append(findings, Finding{
					Rule:     "graphql_action_no_controller",
					Severity: SeverityError,
					File:     filepath.Join(policiesDir, e.Name()),
					Line:     lineNumber(src, match[0]),
					Message:  "ControllerAction \"" + actionName + "\" references " + controllerRef + " which was not found in any controller",
				})
			}
		}
	}
	return findings
}

// ruleGraphQLStaleExpose flags models exposed in GraphQL policies whose
// corresponding table was dropped in a later migration.
func ruleGraphQLStaleExpose(ctx *AnalysisContext) []Finding {
	if ctx.GraphQLExposed == nil {
		return nil
	}

	tableNames := map[string]bool{}
	for _, tbl := range ctx.Tables {
		tableNames[tbl.Name] = true
	}

	var findings []Finding
	for model := range ctx.GraphQLExposed {
		if !tableNames[model] {
			// Already caught by graphql_exposed_no_migration if never existed.
			// This rule specifically targets tables that existed and were dropped.
			// We check if the model name appears as a dropped table.
			policiesDir := filepath.Join(ctx.ProjectDir, "database", "policies", "graphql")
			if entries, err := os.ReadDir(policiesDir); err == nil {
				for _, e := range entries {
					if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_gen.go") {
						continue
					}
					data, _ := os.ReadFile(filepath.Join(policiesDir, e.Name()))
					src := string(data)
					if strings.Contains(src, "Expose(\""+model+"\"") {
						findings = append(findings, Finding{
							Rule:     "graphql_stale_expose",
							Severity: SeverityWarning,
							File:     filepath.Join(policiesDir, e.Name()),
							Line:     lineNumber(src, "Expose(\""+model+"\""),
							Message:  "model \"" + model + "\" is exposed in GraphQL policy but the table no longer exists — the exposure has no effect",
						})
					}
				}
			}
		}
	}
	return findings
}

// ruleGraphQLExposedNoAuth flags models exposed via GraphQL policies that have
// no auth protection: no owner column, no OwnerSees annotation, and no role-based
// visibility annotation on any column. Severity is error because unprotected
// write operations over GraphQL are a security concern.
func ruleGraphQLExposedNoAuth(ctx *AnalysisContext) []Finding {
	if ctx.GraphQLExposed == nil {
		return nil
	}

	tableByName := map[string]*schema.Table{}
	for _, tbl := range ctx.Tables {
		tableByName[tbl.Name] = tbl
	}

	var findings []Finding
	for model := range ctx.GraphQLExposed {
		tbl, ok := tableByName[model]
		if !ok {
			continue // table not found — caught by graphql_exposed_no_migration
		}

		hasOwner := false
		hasRoleAnnotation := false
		hasOwnerSees := false
		for _, col := range tbl.Columns {
			if col.IsOwnerColumn {
				hasOwner = true
			}
			if len(col.VisibleTo) > 0 {
				hasRoleAnnotation = true
			}
			if col.IsOwnerSees {
				hasOwnerSees = true
			}
		}

		if !hasOwner && !hasRoleAnnotation && !hasOwnerSees {
			findings = append(findings, Finding{
				Rule:     "graphql_exposed_no_auth",
				Severity: SeverityError,
				File:     model,
				Line:     1,
				Message:  "model \"" + model + "\" is exposed via GraphQL but has no owner column, no role visibility annotations, and no OwnerSees — mutations will be unprotected",
			})
		}
	}
	return findings
}

// lineNumber returns the 1-based line number of the first occurrence of substr in src.
func lineNumber(src, substr string) int {
	idx := strings.Index(src, substr)
	if idx < 0 {
		return 1
	}
	return strings.Count(src[:idx], "\n") + 1
}

// parseControllerRef extracts "Controller.Method" from "controllers.XxxController{}.Method"
func parseControllerRef(ref string) string {
	ref = strings.TrimSpace(ref)
	// Remove package prefix
	if idx := strings.LastIndex(ref, "."); idx >= 0 {
		// Could be "controllers.UserController{}.Ban"
		parts := strings.Split(ref, ".")
		if len(parts) >= 3 {
			controller := strings.TrimSuffix(parts[1], "{}")
			method := parts[2]
			return controller + "." + method
		}
		if len(parts) == 2 {
			controller := strings.TrimSuffix(parts[0], "{}")
			method := parts[1]
			return controller + "." + method
		}
	}
	return ""
}

// findClosureEndSqueeze finds the matching closing brace after a position.
func findClosureEndSqueeze(src string, start int) int {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}
