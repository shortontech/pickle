package squeeze

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/shortontech/pickle/pkg/generator"
)

// Run executes all enabled squeeze rules against the project and returns findings.
func Run(projectDir string) ([]Finding, error) {
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
		return nil, fmt.Errorf("parsing controllers: %w", err)
	}

	// 5. Scan request structs
	requests, err := generator.ScanRequests(project.Layout.RequestsDir)
	if err != nil {
		return nil, fmt.Errorf("scanning requests: %w", err)
	}

	// 6. Get schema from migrations
	tables, _, err := generator.RunSchemaInspector(project)
	if err != nil {
		// Schema inspection is optional — warn and continue
		fmt.Printf("  warning: schema inspection failed: %v\n", err)
		tables = nil
	}

	// 7. Build analysis context
	actx := &AnalysisContext{
		Routes:   routes,
		Methods:  methods,
		Requests: requests,
		Tables:   tables,
		Config:   cfg.Squeeze,
	}

	// 8. Run enabled rules
	var findings []Finding
	for name, rule := range AllRules() {
		if !cfg.Squeeze.RuleEnabled(name) {
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
