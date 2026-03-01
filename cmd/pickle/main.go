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
	case "make:controller":
		cmdMakeController()
	case "make:migration":
		cmdMakeMigration()
	case "make:request":
		cmdMakeRequest()
	case "make:middleware":
		cmdMakeMiddleware()
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
  make:controller   Scaffold a new controller
  make:migration    Scaffold a new migration
  make:request      Scaffold a new request class
  make:middleware    Scaffold a new middleware
  squeeze            Run static analysis on your Pickle project

Options:
  --project <dir>   Project directory (default: current directory)
  --help, -h        Show this help
  --version, -v     Show version`)
}

func cmdCreate() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: pickle create <project-name>\n")
		os.Exit(1)
	}

	projectName := os.Args[2]
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
	// Determine project directory (current working directory or --project flag)
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

	// Find the pickle package directory (where cooked/ and templates live)
	picklePkgDir := findPicklePkgDir()

	fmt.Printf("pickle generate: %s\n", project.Dir)
	if err := generator.Generate(project, picklePkgDir); err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("pickle: done")
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
	args := os.Args[1:]
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

	// Run an initial generate
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
		} else if !strings.HasPrefix(args[i], "-") && name == "" {
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

func cmdSqueeze() {
	projectDir := "."
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			projectDir = args[i+1]
			i++
		}
	}

	fmt.Printf("pickle squeeze: %s\n", projectDir)
	findings, err := squeeze.Run(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pickle: %v\n", err)
		os.Exit(1)
	}

	if len(findings) == 0 {
		fmt.Println("\n  \033[32m✓ No findings\033[0m")
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

	fmt.Printf("\n  %d error(s), %d warning(s)\n", errors, warnings)
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

	// Fallback: look in GOPATH/pkg/mod for the pickle module
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}

	// This is a simplification — in practice we'd need the exact version
	fmt.Fprintf(os.Stderr, "pickle: warning: could not locate pkg/ directory, some generators may be skipped\n")
	return filepath.Join(gopath, "pkg", "mod", "github.com", "pickle-framework", "pickle", "pkg")
}
