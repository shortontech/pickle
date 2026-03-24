package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchDirs are the directories pickle watches for changes (Laravel layout).
var WatchDirs = []string{
	"app/http/controllers",
	"app/http/middleware",
	"app/http/requests",
	"app/models",
	"database/migrations",
	"routes",
	"config",
}

// WatchDirsForServices returns the watch directories for a multi-service project.
// serviceDirs are relative paths like "services/api", "services/worker".
func WatchDirsForServices(serviceDirs []string) []string {
	dirs := []string{
		"database/migrations",
		"config",
		"app/models",
	}
	for _, svc := range serviceDirs {
		dirs = append(dirs,
			filepath.Join(svc, "http", "controllers"),
			filepath.Join(svc, "http", "middleware"),
			filepath.Join(svc, "http", "requests"),
			filepath.Join(svc, "routes"),
		)
	}
	return dirs
}

// OnChange is called when relevant files change. The argument is a list
// of changed paths (deduplicated over the debounce window).
type OnChange func(changed []string)

// WatchWithDirs monitors specific directories for changes. Like Watch but
// accepts a custom list of relative directories instead of using WatchDirs.
func WatchWithDirs(projectDir string, dirs []string, onChange OnChange) error {
	return watchImpl(projectDir, dirs, onChange)
}

// Watch monitors a project directory for changes to controllers, migrations,
// requests, middleware, and routes.go. It calls onChange after a debounce
// period when changes are detected. Watch blocks until ctx is cancelled
// or an unrecoverable error occurs.
func Watch(projectDir string, onChange OnChange) error {
	return watchImpl(projectDir, WatchDirs, onChange)
}

func watchImpl(projectDir string, watchDirs []string, onChange OnChange) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	// Watch conventional directories
	watchedCount := 0
	for _, dir := range watchDirs {
		path := filepath.Join(projectDir, dir)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := addRecursive(w, path); err != nil {
				return fmt.Errorf("watching %s: %w", dir, err)
			}
			fmt.Printf("  watching %s/\n", dir)
			watchedCount++
		}
	}

	if watchedCount == 0 {
		return fmt.Errorf("no watchable directories found in %s (expected: %v)", projectDir, watchDirs)
	}

	// Debounce: collect changes over 100ms before triggering
	const debounce = 100 * time.Millisecond
	timer := time.NewTimer(debounce)
	timer.Stop()
	pending := map[string]bool{}

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}

			if !isRelevant(event) {
				continue
			}

			pending[event.Name] = true
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)

			// If a new directory was created, start watching it
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addRecursive(w, event.Name); err != nil {
						fmt.Fprintf(os.Stderr, "pickle watch: failed to watch new directory %s: %v\n", event.Name, err)
					}
				}
			}

		case <-timer.C:
			if len(pending) == 0 {
				continue
			}

			changed := make([]string, 0, len(pending))
			for path := range pending {
				changed = append(changed, path)
			}
			pending = map[string]bool{}

			onChange(changed)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "pickle watch error: %v\n", err)
		}
	}
}

// AppWatchConfig describes an app's directories for monorepo watching.
type AppWatchConfig struct {
	Name          string
	ProjectDir    string
	MigrationDirs []string // absolute paths to all migration dirs (including shared)
}

// WatchMonorepo monitors multiple apps for changes. When a shared migration
// directory changes, all apps referencing it are regenerated. When an
// app-specific directory changes, only that app is regenerated.
func WatchMonorepo(rootDir string, apps []AppWatchConfig, onChange func(appName string, changed []string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	// Build reverse map: absolute dir path → app names that care about it
	dirToApps := map[string][]string{}

	for _, app := range apps {
		// Watch standard app directories
		for _, rel := range WatchDirs {
			path := filepath.Join(app.ProjectDir, rel)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				if err := addRecursive(w, path); err != nil {
					return fmt.Errorf("watching %s/%s: %w", app.Name, rel, err)
				}
				dirToApps[path] = appendUnique(dirToApps[path], app.Name)
				fmt.Printf("  [%s] watching %s/\n", app.Name, rel)
			}
		}
		// Watch shared migration directories
		for _, migDir := range app.MigrationDirs {
			if _, already := dirToApps[migDir]; !already {
				if info, err := os.Stat(migDir); err == nil && info.IsDir() {
					if err := addRecursive(w, migDir); err != nil {
						return fmt.Errorf("watching shared %s: %w", migDir, err)
					}
				}
			}
			dirToApps[migDir] = appendUnique(dirToApps[migDir], app.Name)
		}
	}

	// Debounce per app
	const debounce = 100 * time.Millisecond
	timer := time.NewTimer(debounce)
	timer.Stop()
	pending := map[string]map[string]bool{} // appName → set of changed paths

	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !isRelevant(event) {
				continue
			}

			// Determine which apps are affected by this path
			affected := resolveAffectedApps(event.Name, dirToApps)
			for _, appName := range affected {
				if pending[appName] == nil {
					pending[appName] = map[string]bool{}
				}
				pending[appName][event.Name] = true
			}

			if len(affected) > 0 {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}

			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := addRecursive(w, event.Name); err != nil {
						fmt.Fprintf(os.Stderr, "pickle watch: failed to watch new directory %s: %v\n", event.Name, err)
					}
				}
			}

		case <-timer.C:
			for appName, paths := range pending {
				changed := make([]string, 0, len(paths))
				for p := range paths {
					changed = append(changed, p)
				}
				onChange(appName, changed)
			}
			pending = map[string]map[string]bool{}

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "pickle watch error: %v\n", err)
		}
	}
}

// resolveAffectedApps finds which apps are affected by a changed file path
// by checking which registered directories are parents of the changed path.
func resolveAffectedApps(changedPath string, dirToApps map[string][]string) []string {
	seen := map[string]bool{}
	var result []string
	for dir, apps := range dirToApps {
		if strings.HasPrefix(changedPath, dir+string(filepath.Separator)) || changedPath == dir {
			for _, app := range apps {
				if !seen[app] {
					seen[app] = true
					result = append(result, app)
				}
			}
		}
	}
	return result
}

func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// addRecursive adds a directory and all subdirectories to the watcher.
func addRecursive(w *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(d.Name(), ".") && path != dir {
				return filepath.SkipDir
			}
			return w.Add(path)
		}
		return nil
	})
}

// isRelevant filters events to only Go source file changes.
func isRelevant(event fsnotify.Event) bool {
	// Only care about writes, creates, and renames
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
		return false
	}

	// Only care about .go files (or directories for create events)
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			return true
		}
	}

	return strings.HasSuffix(event.Name, ".go")
}
