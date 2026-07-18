package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// ScanCommands parses Go files in the commands directory and returns the names
// of exported types that implement the Command interface (Name(), Description(), Run([]string) error).
func ScanCommands(commandsDir string) ([]string, error) {
	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		return nil, err
	}

	var commands []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		// Skip generated files
		if strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, commandsDir+"/"+e.Name(), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", e.Name(), err)
		}

		commands = append(commands, findCommandTypes(f)...)
	}

	return commands, nil
}

// findCommandTypes returns exported type names that have Name() string,
// Description() string, and Run([]string) error methods declared in the file.
func findCommandTypes(f *ast.File) []string {
	// Collect all exported struct type names
	structNames := map[string]bool{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ts.Name.IsExported() {
				continue
			}
			if _, ok := ts.Type.(*ast.StructType); ok {
				structNames[ts.Name.Name] = true
			}
		}
	}

	// Check which structs have all three methods
	methods := map[string]map[string]bool{} // type -> method set
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}

		typeName := receiverTypeName(fn.Recv.List[0].Type)
		if typeName == "" || !structNames[typeName] {
			continue
		}

		switch fn.Name.Name {
		case "Name", "Description", "Run":
			if methods[typeName] == nil {
				methods[typeName] = map[string]bool{}
			}
			methods[typeName][fn.Name.Name] = true
		}
	}

	var result []string
	for name, ms := range methods {
		if ms["Name"] && ms["Description"] && ms["Run"] {
			result = append(result, name)
		}
	}
	return result
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// ScanRouteVars scans Go files in routesDir for exported var declarations
// that call pickle.Routes(...) and returns their names (e.g. ["API"]).
func ScanRouteVars(routesDir string) ([]string, error) {
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		return nil, err
	}

	var vars []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, routesDir+"/"+e.Name(), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", e.Name(), err)
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) == 0 || !vs.Names[0].IsExported() {
					continue
				}
				// Check if the value is a call to something ending in "Routes"
				if len(vs.Values) == 1 {
					if call, ok := vs.Values[0].(*ast.CallExpr); ok {
						funcName := ""
						switch fn := call.Fun.(type) {
						case *ast.Ident:
							funcName = fn.Name
						case *ast.SelectorExpr:
							funcName = fn.Sel.Name
						}
						if funcName == "Routes" {
							vars = append(vars, vs.Names[0].Name)
						}
					}
				}
			}
		}
	}
	return vars, nil
}

// warnNonControllerHandlers scans route files and prints advisory warnings if any
// handler references a type from a package other than "controllers".
func warnNonControllerHandlers(routesDir string) {
	for _, warning := range findNonControllerHandlers(routesDir) {
		fmt.Printf("  warning: %s:%d: handler from package %q, expected \"controllers\"\n", warning.file, warning.line, warning.packageName)
	}
}

type nonControllerHandlerWarning struct {
	file        string
	line        int
	packageName string
}

func findNonControllerHandlers(routesDir string) []nonControllerHandlerWarning {
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		return nil
	}
	var warnings []nonControllerHandlerWarning
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		path := filepath.Join(routesDir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if !isRouteRegistrationMethod(method.Sel.Name) || len(call.Args) < 2 {
				return true
			}
			comp := handlerCompositeLiteral(call.Args[1])
			if comp == nil {
				return true
			}
			sel, ok := comp.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name != "controllers" && ident.Name != "" {
				pos := fset.Position(comp.Pos())
				warnings = append(warnings, nonControllerHandlerWarning{file: pos.Filename, line: pos.Line, packageName: ident.Name})
			}
			return true
		})
	}
	return warnings
}

func isRouteRegistrationMethod(name string) bool {
	switch name {
	case "Get", "Post", "Put", "Patch", "Delete", "Resource":
		return true
	default:
		return false
	}
}

func handlerCompositeLiteral(expr ast.Expr) *ast.CompositeLit {
	switch value := expr.(type) {
	case *ast.SelectorExpr:
		return handlerCompositeLiteral(value.X)
	case *ast.CompositeLit:
		return value
	case *ast.ParenExpr:
		return handlerCompositeLiteral(value.X)
	case *ast.UnaryExpr:
		return handlerCompositeLiteral(value.X)
	default:
		return nil
	}
}

var commandsGlueTemplate = template.Must(template.New("commands").Parse(`// Code generated by Pickle. DO NOT EDIT.
package commands

import (
	{{ if .HasSeeders }}"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"strings"
	{{ end }}
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	pickle "{{ .HTTPImport }}"
	"{{ .ModelsImport }}"
	"{{ .MigrationsImport }}"
	"{{ .ConfigImport }}"
	"{{ .RoutesImport }}"
{{ if .HasSeeders }}	"{{ .SeedersImport }}"
	"golang.org/x/crypto/bcrypt"
{{ end }}
{{ if .HasAuth }}	"{{ .AuthImport }}"
{{ end }}{{ if .HasSchedule }}	"{{ .ScheduleImport }}"
{{ end }}{{ if .HasPolicies }}	"{{ .PoliciesImport }}"
{{ end }})

// migrateCommand runs pending migrations.
type migrateCommand struct{}

func (c migrateCommand) Name() string        { return "migrate" }
func (c migrateCommand) Description() string { return "Run pending migrations" }
func (c migrateCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
	if err := runner.Migrate(migrations.Registry); err != nil { return err }
{{ if .HasPolicies }}	policyRunner := policies.NewPolicyRunner(models.DB, config.Database.Connection().Driver)
	return policyRunner.Migrate(policies.PolicyRegistry)
{{ else }}	return nil
{{ end }}
}

// migrateRollbackCommand rolls back the last batch.
type migrateRollbackCommand struct{}

func (c migrateRollbackCommand) Name() string        { return "migrate:rollback" }
func (c migrateRollbackCommand) Description() string { return "Roll back the last migration batch" }
func (c migrateRollbackCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
	return runner.Rollback(migrations.Registry)
}

// migrateFreshCommand drops all tables and re-runs migrations.
type migrateFreshCommand struct{}

func (c migrateFreshCommand) Name() string        { return "migrate:fresh" }
func (c migrateFreshCommand) Description() string { return "Drop all tables and re-run all migrations" }
func (c migrateFreshCommand) Run(args []string) error {
{{ if .HasSeeders }}	seed := false
	seedArgs := make([]string, 0, len(args))
	for _, argument := range args {
		if argument == "--seed" { seed = true; continue }
		seedArgs = append(seedArgs, argument)
	}
	if seed { if err := migrations.ValidateSeedAnchorArguments(seedArgs); err != nil { return err } }
{{ end }}	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
	if err := runner.Fresh(migrations.Registry); err != nil { return err }
{{ if .HasPolicies }}	policyRunner := policies.NewPolicyRunner(models.DB, config.Database.Connection().Driver)
	if err := policyRunner.Migrate(policies.PolicyRegistry); err != nil { return err }
{{ end }}
{{ if .HasSeeders }}	if seed { return dbSeedCommand{}.Run(seedArgs) }
{{ end }}	return nil
}

// migrateStatusCommand shows migration status.
type migrateStatusCommand struct{}

func (c migrateStatusCommand) Name() string        { return "migrate:status" }
func (c migrateStatusCommand) Description() string { return "Show migration status" }
func (c migrateStatusCommand) Run(args []string) error {
	runner := migrations.NewRunner(models.DB, config.Database.Connection().Driver)
	statuses, err := runner.Status(migrations.Registry)
	if err != nil {
		return err
	}
	migrations.PrintStatus(statuses)
{{ if .HasPolicies }}	policyRunner := policies.NewPolicyRunner(models.DB, config.Database.Connection().Driver)
	policyStatuses, err := policyRunner.Status(policies.PolicyRegistry)
	if err != nil { return err }
	policies.PrintStatus(policyStatuses)
	rowStatuses, err := policyRunner.RowPolicyStatus()
	if err != nil { return err }
	policies.PrintRowPolicyStatus(rowStatuses)
{{ end }}
	return nil
}

{{ if .HasPolicies }}type policiesStatusCommand struct{}
func (c policiesStatusCommand) Name() string { return "policies:status" }
func (c policiesStatusCommand) Description() string { return "Show role policy and generated row-policy status" }
func (c policiesStatusCommand) Run(args []string) error {
	runner := policies.NewPolicyRunner(models.DB, config.Database.Connection().Driver)
	statuses, err := runner.Status(policies.PolicyRegistry)
	if err != nil { return err }
	policies.PrintStatus(statuses)
	rowStatuses, err := runner.RowPolicyStatus()
	if err != nil { return err }
	policies.PrintRowPolicyStatus(rowStatuses)
	return nil
}

type policiesRollbackCommand struct{}
func (c policiesRollbackCommand) Name() string { return "policies:rollback" }
func (c policiesRollbackCommand) Description() string { return "Roll back the last role policy batch" }
func (c policiesRollbackCommand) Run(args []string) error {
	runner := policies.NewPolicyRunner(models.DB, config.Database.Connection().Driver)
	return runner.Rollback(policies.PolicyRegistry)
}

type rlsStatusCommand struct{}
func (c rlsStatusCommand) Name() string { return "rls:status" }
func (c rlsStatusCommand) Description() string { return "Inspect generated PostgreSQL RLS drift and runtime bypass privileges" }
func (c rlsStatusCommand) Run(args []string) error {
	runner := policies.NewPolicyRunner(models.DB, config.Database.Connection().Driver)
	statuses, err := runner.RowPolicyStatus()
	if err != nil { return err }
	policies.PrintRowPolicyStatus(statuses)
	return nil
}
{{ end }}

{{ if .HasSeeders }}// dbSeedCommand runs an explicit root seed scenario.
type dbSeedCommand struct{}

func (c dbSeedCommand) Name() string        { return "db:seed" }
func (c dbSeedCommand) Description() string { return "Seed the database" }
func (c dbSeedCommand) Run(args []string) error {
	flags := flag.NewFlagSet("db:seed", flag.ContinueOnError)
	rootSeed := flags.Int64("seed", 0, "deterministic 64-bit root seed")
	list := flags.Bool("list", false, "list root scenarios")
	dryRun := flags.Bool("dry-run", false, "plan without inserting")
	var asOf migrations.SeedAnchorFlag
	flags.Var(&asOf, "as-of", "explicit deterministic RFC3339 time anchor")
	force := flags.Bool("force", false, "permit a confirmed non-development environment")
	confirmEnvironment := flags.String("confirm-environment", "", "exact environment mutation confirmation")
	var flagArgs, scenarioArgs []string
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if !strings.HasPrefix(argument, "-") {
			scenarioArgs = append(scenarioArgs, argument)
			continue
		}
		flagArgs = append(flagArgs, argument)
		if (argument == "--seed" || argument == "--as-of" || argument == "--confirm-environment") && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if err := flags.Parse(flagArgs); err != nil { return err }
	names := seeders.Names()
	if *list {
		for _, name := range names { fmt.Println(name) }
		return nil
	}
	if len(names) == 0 { return fmt.Errorf("no root seed scenarios are defined") }
	scenario := names[0]
	if len(scenarioArgs) > 1 { return fmt.Errorf("db:seed accepts at most one scenario name") }
	if len(scenarioArgs) == 1 { scenario = scenarioArgs[0] }
	anchor := asOf.Anchor()
	if *rootSeed == 0 {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil { return fmt.Errorf("generate root seed: %w", err) }
		*rootSeed = int64(binary.BigEndian.Uint64(raw[:]))
	}
	fmt.Printf("Seed: %d\n", *rootSeed)
	fmt.Printf("As of: %s\n", anchor.UTC().Format(time.RFC3339))
	definition, err := seeders.Resolve(scenario)
	if err != nil { return err }
	executor := migrations.SeedExecutor{DB: models.DB, Tables: seeders.Tables()}
	result, err := executor.Run(context.Background(), definition.Graph, migrations.SeedExecutionOptions{
		Scenario: scenario,
		RootSeed: *rootSeed,
		Environment: config.App.Env,
		Force: *force,
		ConfirmEnvironment: *confirmEnvironment,
		DryRun: *dryRun,
		Driver: config.Database.Connection().Driver,
		Policy: definition.Policy,
		SeederResolver: seeders.ResolveValue,
		AnchorTime: anchor,
		PasswordHasher: func(value string) (string, error) {
			hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
			return string(hash), err
		},
	})
	if err != nil { return err }
	if result.DryRun {
		for _, row := range result.Rows { if row.IntegrityDerived { fmt.Println("Integrity: framework-derived; live chain head is resolved only during execution"); break } }
	}
	if result.DryRun { fmt.Printf("Plan: %s (%d rows)\n", result.Scenario, len(result.Rows)) } else { fmt.Printf("Seeded: %s (%d rows)\n", result.Scenario, len(result.Rows)) }
	return nil
}
{{ end }}

// BuiltinCommands returns the built-in Pickle commands.
func BuiltinCommands() []pickle.Command {
	return []pickle.Command{
		migrateCommand{},
		migrateRollbackCommand{},
		migrateFreshCommand{},
		migrateStatusCommand{},
{{ if .HasPolicies }}		policiesStatusCommand{},
		policiesRollbackCommand{},
		rlsStatusCommand{},
{{ end }}
{{ if .HasSeeders }}		dbSeedCommand{},
{{ end }}	}
}

// UserCommands returns user-defined commands.
func UserCommands() []pickle.Command {
	return []pickle.Command{
{{ range .UserCommands }}		{{ . }}{},
{{ end }}	}
}

// Commands returns all commands (built-in + user-defined).
func Commands() []pickle.Command {
	return append(BuiltinCommands(), UserCommands()...)
}

// NewApp creates the application with config, database, routes, and commands wired up.
func NewApp() *pickle.App {
	return pickle.BuildApp(
		func() {
			config.Init()
			models.DB = config.Database.Open()
			models.DatabaseDriver = config.Database.Connection().Driver
{{ if .HasAuth }}			auth.Init(config.Env, models.DB)
{{ if .HasPolicies }}			pickle.RegisterHTTPPolicyAuthenticator(func(r *http.Request) (any, *pickle.AuthInfo, error) {
				source, present, err := auth.TryAuthenticatePolicySource(r)
				if err != nil { return nil, nil, err }
				if !present { return models.PublicPolicyContext(), nil, nil }
				identities := source.PolicyIdentities()
				roles := source.PolicyRoles()
				role := ""
				if len(roles) > 0 { role = roles[0] }
				return models.PolicyContextFromVerified(source), &pickle.AuthInfo{UserID: identities["user_id"], Role: role}, nil
			})
{{ end }}
{{ end }}		},
		func() {
{{ if .HasSchedule }}			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// Start the scheduler in a background goroutine
			go schedule.Schedule.Start(ctx)
{{ else }}			_, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

{{ end }}
			mux := http.NewServeMux()
{{ range .RouteVars }}			routes.{{ . }}.RegisterRoutes(mux)
{{ end }}			log.Printf("listening on :%s", config.App.Port)
			srv := &http.Server{
				Addr:              ":" + config.App.Port,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      60 * time.Second,
				IdleTimeout:       120 * time.Second,
			}
			if err := srv.ListenAndServe(); err != nil {
				log.Fatal(err)
			}
		},
		Commands()...,
	)
}
`))

// GenerateCommandsGlue produces app/commands/pickle_gen.go.
func GenerateCommandsGlue(modulePath string, migrationsRel string, userCommands []string, routeVars []string, hasAuth bool, hasSchedule bool, features ...bool) ([]byte, error) {
	// Default to "API" if no route vars found
	if len(routeVars) == 0 {
		routeVars = []string{"API"}
	}

	data := struct {
		HTTPImport       string
		ModelsImport     string
		MigrationsImport string
		ConfigImport     string
		RoutesImport     string
		AuthImport       string
		ScheduleImport   string
		SeedersImport    string
		PoliciesImport   string
		UserCommands     []string
		RouteVars        []string
		HasAuth          bool
		HasSchedule      bool
		HasSeeders       bool
		HasPolicies      bool
	}{
		HTTPImport:       modulePath + "/app/http",
		ModelsImport:     modulePath + "/app/models",
		MigrationsImport: modulePath + "/" + migrationsRel,
		ConfigImport:     modulePath + "/config",
		RoutesImport:     modulePath + "/routes",
		AuthImport:       modulePath + "/app/http/auth",
		ScheduleImport:   modulePath + "/schedule",
		SeedersImport:    modulePath + "/database/seeders",
		PoliciesImport:   modulePath + "/database/policies",
		UserCommands:     userCommands,
		RouteVars:        routeVars,
		HasAuth:          hasAuth,
		HasSchedule:      hasSchedule,
		HasSeeders:       len(features) > 0 && features[0],
		HasPolicies:      len(features) > 1 && features[1],
	}

	var buf bytes.Buffer
	if err := commandsGlueTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("commands template: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("go format commands glue: %w\n%s", err, buf.String())
	}
	return formatted, nil
}
