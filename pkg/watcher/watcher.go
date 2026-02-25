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

// OnChange is called when relevant files change. The argument is a list
// of changed paths (deduplicated over the debounce window).
type OnChange func(changed []string)

// Watch monitors a project directory for changes to controllers, migrations,
// requests, middleware, and routes.go. It calls onChange after a debounce
// period when changes are detected. Watch blocks until ctx is cancelled
// or an unrecoverable error occurs.
func Watch(projectDir string, onChange OnChange) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	// Watch conventional directories
	for _, dir := range WatchDirs {
		path := filepath.Join(projectDir, dir)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := addRecursive(w, path); err != nil {
				return fmt.Errorf("watching %s: %w", dir, err)
			}
			fmt.Printf("  watching %s/\n", dir)
		}
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
			timer.Reset(debounce)

			// If a new directory was created, start watching it
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					addRecursive(w, event.Name)
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
