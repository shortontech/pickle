package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shortontech/pickle/pkg/generator"
	picklemcp "github.com/shortontech/pickle/pkg/mcp"
	"github.com/shortontech/pickle/pkg/scaffold"
	"github.com/shortontech/pickle/pkg/squeeze"
	"github.com/shortontech/pickle/pkg/watcher"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	// Handle --watch flag anywhere in args
	for _, arg := range os.Args[1:] {
		if arg == "--watch" {
			cmdWatch()
			return
		}
	}

	switch os.Args[1] {
	case "create":
		cmdCreate()
	case "generate":
		cmdGenerate()
	case "mcp":
		cmdMCP()
	case "migrate", "migrate:rollback", "migrate:fresh", "migrate:status":
		cmdMigrate()
	case "policies:rollback", "policies:status":
		cmdMigrate()
	case "graphql:rollback", "graphql:status":
		cmdMigrate()
	case "graphql:schema":
		cmdGraphQLSchema()
	case "make:controller":
		cmdMakeController()
	case "make:migration":
		cmdMakeMigration()
	case "make:request":
		cmdMakeRequest()
	case "make:middleware":
		cmdMakeMiddleware()
	case "make:job":
		cmdMakeJob()
	case "make:policy":
		cmdMakePolicy()
	case "make:action":
		cmdMakeAction()
	case "make:scope":
		cmdMakeScope()
	case "make:graphql-policy":
		cmdMakeGraphQLPolicy()
	case "squeeze":
		cmdSqueeze()
	case "--help", "-h", "help":
		usage()
	case "--version", "-v", "version":
		fmt.Println("pickle v0.1.0")
	default:
		fmt.Fprintf(os.Stderr, "pickle: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`Usage: pickle <command>

Commands:
  create <name>     Create a new Pickle project
  generate          Generate all files from project sources
  --watch           Watch for changes and regenerate on save
  mcp               Start the MCP server (stdio transport)
  mcp --http :9921  Start the MCP server (SSE over HTTP)
  migrate           Run all pending migrations
  migrate:rollback  Roll back the last batch of migrations
  migrate:fresh     Drop all tables and re-run all migrations
  migrate:status    Show migration status
  policies:rollback Roll back the last batch of role policies
  policies:status   Show role policy status
  graphql:rollback  Roll back the last batch of GraphQL policies
  graphql:status    Show GraphQL policy status
  make:controller   Scaffold a new controller
  make:migration    Scaffold a new migration
  make:request      Scaffold a new request class
  make:middleware    Scaffold a new middleware
  make:job              Scaffold a new job
  make:policy          Scaffold a new role policy
  make:action          Scaffold a new action + gate (model/action)
  make:scope           Scaffold a new scope (model/scope)
  make:graphql-policy  Scaffold a new GraphQL policy
  graphql:schema       Print the current GraphQL SDL
  squeeze              Run static analysis on your Pickle project

Options:
  --project <dir>   Project directory (default: current directory)
  --app <name>      Target a specific app in a monorepo (requires pickle.yaml with apps)
  --help, -h        Show this help
  --version, -v     Show version`)
}

func cmdCreate() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: pickle create <project-name>\n")
		os.Exit(1)
	}

	projectName := os.Args[2]

	// Validate project name: must be a simple name, not a path
	if strings.Contains(projectName, "..") || strings.ContainsAny(projectName, "/\\") {
		fmt.Fprintf(os.Stderr, "pickle: project name %q must not contain path separators or '..'\n", projectName)
		os.Exit(1)
	}

	targetDir, err := filepath.Abs(projectName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	// Use project name as module name, allow override with --module
	moduleName := projectName
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--module" && i+1 < len(args) {
			moduleName = args[i+1]
			i++
		}
	}

	if _, err := os.Stat(targetDir); err == nil {
		fmt.Fprintf(os.Stderr, "pickle: directory %q already exists\n", projectName)
		os.Exit(1)
	}

	fmt.Printf("pickle create: %s\n", projectName)
	if err := scaffold.Create(moduleName, targetDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	// Run generator first so all Go files exist before go mod tidy
	fmt.Println("  generating...")
	project, err := generator.DetectProject(targetDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	picklePkgDir := findPicklePkgDir()
	if err := generator.Generate(project, picklePkgDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: generate failed: %v\n", err)
		fmt.Println("  you may need to run 'pickle generate' manually")
	}

	// Run go mod tidy after generation so all imports are present
	fmt.Println("  running go mod tidy...")
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = targetDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: go mod tidy failed: %v\n", err)
		fmt.Println("  you may need to run 'go mod tidy' manually")
	}

	fmt.Printf("\npickle: project %q created successfully!\n", projectName)
	fmt.Printf("  cd %s && pickle --watch\n", projectName)
}

func cmdMCP() {
	projectDir := "."
	httpAddr := ""
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++
			}
		case "--http":
			if i+1 < len(args) {
				httpAddr = args[i+1]
				i++
			}
		}
	}

	server, err := picklemcp.NewServer(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle mcp: %v\n", err)
		os.Exit(1)
	}

	if httpAddr != "" {
		if err := server.RunHTTP(httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "pickle mcp: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := server.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "pickle mcp: %v\n", err)
			os.Exit(1)
		}
	}
}

func cmdGenerate() {
	projectDir := "."
	appFilter := ""
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++
			}
		case "--app":
			if i+1 < len(args) {
				appFilter = args[i+1]
				i++
			}
		}
	}

	// Check for monorepo config
	cfg, err := squeeze.LoadConfig(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	if cfg.IsMonorepo() {
		picklePkgDir := findPicklePkgDir()
		for name, appCfg := range cfg.Apps {
			if appFilter != "" && name != appFilter {
				continue
			}
			project, err := projectFromAppConfig(projectDir, appCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "pickle: app %s: %v\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("pickle generate: [%s] %s\n", name, project.Dir)
			if err := generator.Generate(project, picklePkgDir); err != nil {
				fmt.Fprintf(os.Stderr, "pickle: app %s: %v\n", name, err)
				os.Exit(1)
			}
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = project.Dir
			if out, err := cmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "pickle: app %s: go mod tidy failed: %v\n%s", name, err, out)
			}
		}
		fmt.Println("pickle: done")
		return
	}

	// Multi-service mode: one go.mod, shared models, per-service HTTP/bindings
	if cfg.IsMultiService() {
		project, err := generator.DetectProject(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
			os.Exit(1)
		}

		for name, svc := range cfg.Services {
			absDir := filepath.Join(project.Dir, svc.Dir)
			project.Services = append(project.Services, generator.ServiceLayout{
				Name:        name,
				Dir:         absDir,
				HTTPDir:     filepath.Join(absDir, "http"),
				HTTPPkg:     "pickle",
				RequestsDir: filepath.Join(absDir, "http", "requests"),
				CommandsDir: filepath.Join(absDir, "commands"),
			})
		}

		picklePkgDir := findPicklePkgDir()
		fmt.Printf("pickle generate: %s (%d services)\n", project.Dir, len(project.Services))
		if err := generator.Generate(project, picklePkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
			os.Exit(1)
		}

		cmd := exec.Command("go", "mod", "tidy")
		cmd.Dir = project.Dir
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "pickle: go mod tidy failed: %v\n%s", err, out)
		}

		fmt.Println("pickle: done")
		return
	}

	// Single-service mode
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	picklePkgDir := findPicklePkgDir()

	fmt.Printf("pickle generate: %s\n", project.Dir)
	if err := generator.Generate(project, picklePkgDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = project.Dir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: go mod tidy failed: %v\n%s", err, out)
	}

	fmt.Println("pickle: done")
}

// projectFromAppConfig creates a Project from a monorepo app config entry.
func projectFromAppConfig(rootDir string, appCfg squeeze.AppConfig) (*generator.Project, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	appDir := filepath.Join(absRoot, appCfg.Path)
	project, err := generator.DetectProject(appDir)
	if err != nil {
		return nil, err
	}

	// Resolve migration directories
	migrations := appCfg.Migrations
	if len(migrations) == 0 {
		migrations = []string{"database/migrations"}
	}
	for _, rel := range migrations {
		absDir := filepath.Clean(filepath.Join(appDir, rel))
		importPath := generator.ResolveImportPath(appDir, project.ModulePath, absDir)
		project.Layout.MigrationDirs = append(project.Layout.MigrationDirs, generator.MigrationDir{
			Dir:        absDir,
			ImportPath: importPath,
		})
	}

	// Resolve config dir override
	if appCfg.Config != "" {
		project.Layout.ConfigDir = filepath.Clean(filepath.Join(appDir, appCfg.Config))
	}

	return project, nil
}

func cmdMigrate() {
	projectDir := "."
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectDir = args[i+1]
			i++
		}
	}

	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	picklePkgDir := findPicklePkgDir()

	// Always regenerate first so registry_gen.go and runner_gen.go are current
	fmt.Printf("pickle %s: %s\n", os.Args[1], project.Dir)
	fmt.Println("  generating...")
	if err := generator.Generate(project, picklePkgDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: generate failed: %v\n", err)
		os.Exit(1)
	}

	// Delegate to the project's own binary via go run
	// The binary command name matches the pickle CLI command (e.g. "migrate", "migrate:rollback")
	cmd := exec.Command("go", "run", "./cmd/server/", os.Args[1])
	cmd.Dir = project.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Load .env from project root
	cmd.Env = os.Environ()
	dotEnv := generator.ParseDotEnv(filepath.Join(project.Dir, ".env"))
	for k, v := range dotEnv {
		if os.Getenv(k) == "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
	fmt.Println("pickle: done")
}

func cmdWatch() {
	projectDir := "."
	appFilter := ""
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 < len(args) {
				projectDir = args[i+1]
				i++
			}
		case "--app":
			if i+1 < len(args) {
				appFilter = args[i+1]
				i++
			}
		}
	}

	picklePkgDir := findPicklePkgDir()

	// Check for monorepo config
	cfg, err := squeeze.LoadConfig(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	if cfg.IsMonorepo() {
		absRoot, err := filepath.Abs(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
			os.Exit(1)
		}

		// Build app projects
		type appEntry struct {
			name    string
			project *generator.Project
		}
		var apps []appEntry
		for name, appCfg := range cfg.Apps {
			if appFilter != "" && name != appFilter {
				continue
			}
			project, err := projectFromAppConfig(projectDir, appCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "pickle: app %s: %v\n", name, err)
				os.Exit(1)
			}
			apps = append(apps, appEntry{name: name, project: project})
		}

		// Initial generation
		for _, app := range apps {
			fmt.Printf("pickle --watch: [%s] %s\n", app.name, app.project.Dir)
			fmt.Println("  initial generation...")
			if err := generator.Generate(app.project, picklePkgDir); err != nil {
				fmt.Fprintf(os.Stderr, "pickle: app %s: generate failed: %v\n", app.name, err)
			}
		}

		// Build watch configs
		var watchApps []watcher.AppWatchConfig
		for _, app := range apps {
			var migDirs []string
			for _, md := range app.project.Layout.MigrationDirs {
				migDirs = append(migDirs, md.Dir)
			}
			watchApps = append(watchApps, watcher.AppWatchConfig{
				Name:          app.name,
				ProjectDir:    app.project.Dir,
				MigrationDirs: migDirs,
			})
		}

		fmt.Println("  watching for changes (ctrl+c to stop)")
		if err := watcher.WatchMonorepo(absRoot, watchApps, func(appName string, changed []string) {
			// Find the app project
			for _, app := range apps {
				if app.name == appName {
					fmt.Printf("\n  [%s] changed: %d file(s)\n", appName, len(changed))
					fmt.Println("  regenerating...")
					if err := generator.Generate(app.project, picklePkgDir); err != nil {
						fmt.Fprintf(os.Stderr, "  [%s] error: %v\n", appName, err)
					} else {
						fmt.Printf("  [%s] done\n", appName)
					}
					return
				}
			}
		}); err != nil {
			fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Multi-service mode
	if cfg.IsMultiService() {
		project, err := generator.DetectProject(projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
			os.Exit(1)
		}

		var svcDirs []string
		for name, svc := range cfg.Services {
			absDir := filepath.Join(project.Dir, svc.Dir)
			project.Services = append(project.Services, generator.ServiceLayout{
				Name:        name,
				Dir:         absDir,
				HTTPDir:     filepath.Join(absDir, "http"),
				HTTPPkg:     "pickle",
				RequestsDir: filepath.Join(absDir, "http", "requests"),
				CommandsDir: filepath.Join(absDir, "commands"),
			})
			svcDirs = append(svcDirs, svc.Dir)
		}

		fmt.Printf("pickle --watch: %s (%d services)\n", project.Dir, len(project.Services))
		fmt.Println("  initial generation...")
		if err := generator.Generate(project, picklePkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "pickle: generate failed: %v\n", err)
		}

		watchDirs := watcher.WatchDirsForServices(svcDirs)
		fmt.Println("  watching for changes (ctrl+c to stop)")
		if err := watcher.WatchWithDirs(project.Dir, watchDirs, func(changed []string) {
			fmt.Printf("\n  changed: %d file(s)\n", len(changed))
			fmt.Println("  regenerating...")
			if err := generator.Generate(project, picklePkgDir); err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
			} else {
				fmt.Println("  done")
			}
		}); err != nil {
			fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Single-service mode
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("pickle --watch: %s\n", project.Dir)
	fmt.Println("  initial generation...")
	if err := generator.Generate(project, picklePkgDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: generate failed: %v\n", err)
	}

	fmt.Println("  watching for changes (ctrl+c to stop)")
	if err := watcher.Watch(project.Dir, func(changed []string) {
		fmt.Printf("\n  changed: %d file(s)\n", len(changed))
		for _, path := range changed {
			rel, _ := filepath.Rel(project.Dir, path)
			if rel == "" {
				rel = path
			}
			fmt.Printf("    %s\n", rel)
		}

		fmt.Println("  regenerating...")
		if err := generator.Generate(project, picklePkgDir); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		} else {
			fmt.Println("  done")
		}
	}); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
}

func parseMakeArgs() (name, projectDir string) {
	projectDir = "."
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectDir = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "-") {
			fmt.Fprintf(os.Stderr, "pickle: unknown flag %q\n", args[i])
			os.Exit(1)
		} else if name == "" {
			name = args[i]
		}
	}
	return
}

func cmdMakeController() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:controller <Name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeController(name, project.Dir, project.ModulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakeMigration() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:migration <name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeMigration(name, project.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakeRequest() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:request <Name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeRequest(name, project.Dir, project.ModulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakeMiddleware() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:middleware <Name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeMiddleware(name, project.Dir, project.ModulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakeJob() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:job <Name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeJob(name, project.Dir, project.ModulePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakePolicy() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:policy <name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakePolicy(name, project.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakeAction() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:action <model>/<action>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeAction(name, project.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
	// Also print gate file path
	fmt.Printf("  created %s\n", strings.TrimSuffix(relPath, ".go")+"_gate.go")
}

func cmdMakeScope() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:scope <model>/<scope>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeScope(name, project.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdMakeGraphQLPolicy() {
	name, projectDir := parseMakeArgs()
	if name == "" {
		fmt.Fprintf(os.Stderr, "Usage: pickle make:graphql-policy <name>\n")
		os.Exit(1)
	}
	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	relPath, err := scaffold.MakeGraphQLPolicy(name, project.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  created %s\n", relPath)
}

func cmdGraphQLSchema() {
	projectDir := "."
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectDir = args[i+1]
			i++
		}
	}

	project, err := generator.DetectProject(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	picklePkgDir := findPicklePkgDir()

	// Regenerate to ensure schema is current
	fmt.Fprintln(os.Stderr, "  generating...")
	if err := generator.Generate(project, picklePkgDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: generate failed: %v\n", err)
		os.Exit(1)
	}

	// Delegate to the project binary which has access to the compiled SchemaSDL
	cmd := exec.Command("go", "run", "./cmd/server/", "graphql:schema")
	cmd.Dir = project.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Env = os.Environ()
	dotEnv := generator.ParseDotEnv(filepath.Join(project.Dir, ".env"))
	for k, v := range dotEnv {
		if os.Getenv(k) == "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: graphql:schema: %v\n", err)
		os.Exit(1)
	}
}

func cmdSqueeze() {
	projectDir := "."
	hard := false
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectDir = args[i+1]
			i++
		} else if args[i] == "--hard" {
			hard = true
		}
	}

	fmt.Println("\n🥒 Squeezing your pickle...")
	findings, err := squeeze.Run(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	// In --hard mode, promote all warnings to errors
	if hard {
		for i := range findings {
			findings[i].Severity = squeeze.SeverityError
		}
	}

	if len(findings) == 0 {
		fmt.Println("🥒 Your pickle is crunchy.")
		return
	}

	// Print findings grouped by file
	currentFile := ""
	errors, warnings := 0, 0
	for _, f := range findings {
		if f.File != currentFile {
			currentFile = f.File
			fmt.Printf("\n  %s\n", currentFile)
		}

		color := "\033[33m" // yellow for warning
		if f.Severity == squeeze.SeverityError {
			color = "\033[31m" // red for error
			errors++
		} else {
			warnings++
		}
		fmt.Printf("    %sline %d\033[0m [%s] %s\n", color, f.Line, f.Rule, f.Message)
	}

	fmt.Printf("\n🥒 Your pickle is oozing. %d error(s), %d warning(s)\n", errors, warnings)
	if errors > 0 {
		os.Exit(1)
	}
}

// findPicklePkgDir locates the pkg/ directory of the pickle installation.
// When running from source (go run), it uses the source tree.
// When installed (go install), it uses the module cache.
func findPicklePkgDir() string {
	// First try: relative to the binary's source location (development)
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		// thisFile = .../cmd/pickle/main.go
		// pkg dir  = .../pkg/
		srcRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
		pkgDir := filepath.Join(srcRoot, "pkg")
		if _, err := os.Stat(filepath.Join(pkgDir, "cooked")); err == nil {
			return pkgDir
		}
	}

	// Fallback: could not locate the pickle source tree
	fmt.Fprintf(os.Stderr, "pickle: error: could not locate pkg/ directory — generators will not work\n")
	fmt.Fprintf(os.Stderr, "pickle: ensure you are running from the pickle source tree or have pickle installed via go install\n")
	os.Exit(1)
	return "" // unreachable
}
