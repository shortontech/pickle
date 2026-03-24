package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// resolveImportPath determines the Go import path for a directory.
// If absDir is inside the app's module (under appDir), returns modulePath + relative.
// Otherwise, walks up from absDir looking for go.mod to determine the external module path.
func ResolveImportPath(appDir, modulePath, absDir string) string {
	rel, err := filepath.Rel(appDir, absDir)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return modulePath + "/" + filepath.ToSlash(rel)
	}

	// External directory: find the nearest go.mod above absDir
	modRoot, modPath := findModuleRoot(absDir)
	if modRoot != "" {
		sub, err := filepath.Rel(modRoot, absDir)
		if err == nil {
			if sub == "." {
				return modPath
			}
			return modPath + "/" + filepath.ToSlash(sub)
		}
	}

	// Fallback: treat as relative to app module (user needs replace directives)
	return modulePath + "/" + filepath.ToSlash(rel)
}

var moduleDirective = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// findModuleRoot walks up from dir looking for go.mod and returns
// the directory containing it and the module path declared in it.
func findModuleRoot(dir string) (root, modPath string) {
	dir = filepath.Clean(dir)
	for {
		goMod := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(goMod)
		if err == nil {
			match := moduleDirective.FindSubmatch(data)
			if match != nil {
				return dir, string(match[1])
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}
