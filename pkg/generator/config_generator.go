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
	"sort"
	"strings"
	"text/template"
)

// ConfigDef describes a config function discovered in config/*.go.
type ConfigDef struct {
	FuncName   string // unexported function name, e.g. "database"
	ReturnType string // struct type name, e.g. "DatabaseConfig"
	VarName    string // exported var name, e.g. "Database"
}

// ConfigScanResult holds everything discovered from config/*.go.
type ConfigScanResult struct {
	Configs           []ConfigDef
	HasDatabaseConfig bool // user defined DatabaseConfig struct
}

// ScanConfigs parses Go files in configDir and finds unexported functions
// that return a struct type ending in "Config". Also detects known struct types.
func ScanConfigs(configDir string) (*ConfigScanResult, error) {
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("reading config dir: %w", err)
	}

	result := &ConfigScanResult{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") ||
			strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}

		path := filepath.Join(configDir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		for _, decl := range f.Decls {
			// Check for struct type declarations
			if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
				for _, spec := range gen.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						if ts.Name.Name == "DatabaseConfig" {
							result.HasDatabaseConfig = true
						}
					}
				}
			}

			// Check for config functions
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if fn.Name.IsExported() {
				continue
			}
			if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
				continue
			}
			retType, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
			if !ok || !strings.HasSuffix(retType.Name, "Config") {
				continue
			}

			result.Configs = append(result.Configs, ConfigDef{
				FuncName:   fn.Name.Name,
				ReturnType: retType.Name,
				VarName:    exportName(fn.Name.Name),
			})
		}
	}

	sort.Slice(result.Configs, func(i, j int) bool {
		return result.Configs[i].VarName < result.Configs[j].VarName
	})

	return result, nil
}

// exportName capitalizes the first letter of a name.
func exportName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// GenerateConfigGlue produces config/pickle_gen.go with Env(), public vars,
// Init(), and database helper methods if applicable.
func GenerateConfigGlue(scan *ConfigScanResult, packageName string) ([]byte, error) {
	data := configTemplateData{
		Package:      packageName,
		Configs:      scan.Configs,
		Embed:        strings.ReplaceAll(embedCONFIG, packagePlaceholder, packageName),
		HasDBMethods: scan.HasDatabaseConfig,
	}

	var buf bytes.Buffer
	if err := configTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("template execution: %w", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("go format: %w\n---raw output---\n%s", err, buf.String())
	}

	return formatted, nil
}

type configTemplateData struct {
	Package      string
	Configs      []ConfigDef
	Embed        string
	HasDBMethods bool
}

var configTemplate = template.Must(template.New("config").Parse(configTemplateSource))

const configTemplateSource = `{{ .Embed }}
// --- Public config vars (populated by Init) ---
{{ range .Configs }}
var {{ .VarName }} {{ .ReturnType }}
{{ end }}

// Init loads configuration by calling each config function.
// Call this at the start of main().
func Init() {
{{ range .Configs }}	{{ .VarName }} = {{ .FuncName }}()
{{ end }}}
{{ if .HasDBMethods }}
// Connection returns the named connection config, or the default.
func (d DatabaseConfig) Connection(name ...string) ConnectionConfig {
	key := d.Default
	if len(name) > 0 && name[0] != "" {
		key = name[0]
	}
	conn, ok := d.Connections[key]
	if !ok {
		panic("unknown database connection: " + key)
	}
	return conn
}

// Open opens the default (or named) database connection, pings it,
// and returns *sql.DB. Fatals on failure — call at startup.
func (d DatabaseConfig) Open(name ...string) *sql.DB {
	return OpenDB(d.Connection(name...))
}
{{ end }}`
