package squeeze

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
)

// Analyze parses a Pickle project into the shared analysis context used by
// Squeeze rules and by tooling that needs the same framework-aware view.
func Analyze(projectDir string) (*AnalysisContext, error) {
	// 1. Load config
	cfg, err := LoadConfig(projectDir)
	if err != nil {
		return nil, fmt.Errorf("loading pickle.yaml: %w", err)
	}

	// 2. Detect project layout
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		return nil, fmt.Errorf("detecting project: %w", err)
	}

	// 3. Parse routes
	routesDir := filepath.Join(project.Dir, "routes")
	routes, err := ParseRoutes(routesDir)
	if err != nil {
		return nil, fmt.Errorf("parsing routes: %w", err)
	}

	// 4. Parse controllers
	controllersDir := filepath.Join(project.Dir, "app", "http", "controllers")
	methods, err := ParseControllers(controllersDir)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") {
			methods = make(map[string]*ControllerMethod)
		} else {
			return nil, fmt.Errorf("parsing controllers: %w", err)
		}
	}

	// 5. Scan request structs
	requests, err := generator.ScanRequests(project.Layout.RequestsDir)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			requests = nil
		} else {
			return nil, fmt.Errorf("scanning requests: %w", err)
		}
	}

	// 6. Get schema from migrations
	tables, _, _, err := generator.RunSchemaInspector(project)
	if err != nil {
		// Schema inspection is optional — warn and continue
		fmt.Printf("  warning: schema inspection failed: %v\n", err)
		tables = nil
	}

	// 6b. Build function registry for recursive inlining
	funcRegistry := ParseProjectFunctions(project.Dir)

	// 6c. Check for GraphQL and parse custom resolvers
	graphqlDir := filepath.Join(project.Dir, "app", "graphql")
	_, hasGraphQLErr := os.Stat(graphqlDir)
	hasGraphQL := hasGraphQLErr == nil

	if hasGraphQL {
		resolverMethods, resolverErr := ParseControllers(graphqlDir)
		if resolverErr == nil {
			for k, v := range resolverMethods {
				methods[k] = v
			}
		}
		// Also add resolver helper functions to the registry
		resolverFuncs := ParseProjectFunctions(graphqlDir)
		for k, v := range resolverFuncs {
			funcRegistry[k] = v
		}
	}

	// 6d. Scan GraphQL policies to determine which tables are actually exposed
	graphQLExposed := scanGraphQLExposedTables(filepath.Join(project.Dir, "database", "policies", "graphql"))

	return &AnalysisContext{
		Routes:         routes,
		Methods:        methods,
		Requests:       requests,
		Tables:         tables,
		Config:         cfg.Squeeze,
		FuncRegistry:   funcRegistry,
		HasGraphQL:     hasGraphQL,
		GraphQLExposed: graphQLExposed,
		ProjectDir:     projectDir,
	}, nil
}

// Run executes all enabled squeeze rules against the project and returns findings.
func Run(projectDir string) ([]Finding, error) {
	actx, err := Analyze(projectDir)
	if err != nil {
		return nil, err
	}

	// Run enabled rules
	var findings []Finding
	for name, rule := range AllRules() {
		if !actx.Config.RuleEnabled(name) {
			continue
		}
		findings = append(findings, rule(actx)...)
	}

	// 9. Sort by file + line
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	return findings, nil
}

var (
	reExpose   = regexp.MustCompile(`Expose\("([^"]+)"`)
	reUnexpose = regexp.MustCompile(`Unexpose\("([^"]+)"\)`)
)

// scanGraphQLExposedTables reads GraphQL policy files and returns the set of
// model/table names that are exposed. Uses string matching since policy files
// have //go:build ignore and can't be compiled.
func scanGraphQLExposedTables(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	exposed := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		src := string(data)
		for _, match := range reExpose.FindAllStringSubmatch(src, -1) {
			exposed[match[1]] = true
		}
		for _, match := range reUnexpose.FindAllStringSubmatch(src, -1) {
			delete(exposed, match[1])
		}
	}
	if len(exposed) == 0 {
		return nil
	}
	return exposed
}
