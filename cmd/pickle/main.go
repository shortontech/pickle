package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pickle-framework/pickle/pkg/generator"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "generate":
		cmdGenerate()
	case "make:controller":
		fmt.Println("pickle make:controller: not yet implemented")
	case "make:migration":
		fmt.Println("pickle make:migration: not yet implemented")
	case "make:request":
		fmt.Println("pickle make:request: not yet implemented")
	case "make:middleware":
		fmt.Println("pickle make:middleware: not yet implemented")
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
  generate          Generate all files from project sources
  make:controller   Scaffold a new controller
  make:migration    Scaffold a new migration
  make:request      Scaffold a new request class
  make:middleware    Scaffold a new middleware

Options:
  --help, -h        Show this help
  --version, -v     Show version`)
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
