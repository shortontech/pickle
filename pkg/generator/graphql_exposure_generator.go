package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/shortontech/pickle/pkg/schema"
)

// DerivedGraphQLState mirrors migration.DerivedGraphQLState for use at
// generation time without importing the build-constrained migration package.
type DerivedGraphQLState struct {
	Exposures []DerivedExposure
	Actions   []DerivedAction
}

// DerivedExposure represents the computed GraphQL exposure set for a single model.
type DerivedExposure struct {
	Model      string
	Operations []string // "list", "show", "create", "update", "delete"
}

// DerivedAction represents a registered custom controller action.
type DerivedAction struct {
	Name string
}

// ExposedModel pairs a model with its table definition and the GraphQL
// operations that should be generated for it.
type ExposedModel struct {
	Model      string
	Table      *schema.Table
	Operations []string
}

// FilterExposedModels returns only the tables that have been exposed via
// GraphQL policy, matched by model name to table name. Each returned
// ExposedModel includes the operations defined in the policy.
func FilterExposedModels(tables []*schema.Table, state DerivedGraphQLState) []*ExposedModel {
	tableMap := make(map[string]*schema.Table, len(tables))
	for _, t := range tables {
		tableMap[t.Name] = t
	}

	var result []*ExposedModel
	for _, exp := range state.Exposures {
		if len(exp.Operations) == 0 {
			continue
		}
		tbl, ok := tableMap[exp.Model]
		if !ok {
			continue
		}
		result = append(result, &ExposedModel{
			Model:      exp.Model,
			Table:      tbl,
			Operations: exp.Operations,
		})
	}
	return result
}

var (
	reExposeDir          = regexp.MustCompile(`(?m)\.Expose\("([^"]+)"`)
	reAlterExposeDir     = regexp.MustCompile(`(?m)\.AlterExpose\("([^"]+)"`)
	reUnexposeDir        = regexp.MustCompile(`(?m)\.Unexpose\("([^"]+)"\)`)
	reControllerAction   = regexp.MustCompile(`(?m)\.ControllerAction\("([^"]+)"`)
	reRemoveAction       = regexp.MustCompile(`(?m)\.RemoveAction\("([^"]+)"\)`)
	reExposeOps          = regexp.MustCompile(`e\.(List|Show|Create|Update|Delete)\(\)`)
	reRemoveOps          = regexp.MustCompile(`e\.(RemoveList|RemoveShow|RemoveCreate|RemoveUpdate|RemoveDelete)\(\)`)
	reExposeAll          = regexp.MustCompile(`e\.All\(\)`)
)

// DeriveGraphQLStateFromDir reads GraphQL policy files from disk using string
// matching (policy files have //go:build ignore and can't be compiled) and
// derives the current exposure state by replaying operations in file order.
func DeriveGraphQLStateFromDir(dir string) DerivedGraphQLState {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return DerivedGraphQLState{}
	}

	// Sort policy files by name to ensure deterministic ordering (timestamps)
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") ||
			strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	exposures := map[string]map[string]bool{} // model -> ops
	actions := map[string]bool{}
	var modelOrder []string

	for _, fname := range files {
		data, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		src := string(data)

		// Process Expose calls: extract model name and find operations in the closure
		for _, match := range reExposeDir.FindAllStringSubmatchIndex(src, -1) {
			model := src[match[2]:match[3]]
			if exposures[model] == nil {
				exposures[model] = map[string]bool{}
				modelOrder = append(modelOrder, model)
			}
			// Find the ops within the closure (scan ahead for e.Xxx() calls)
			closureEnd := findClosureEnd(src, match[1])
			closure := src[match[1]:closureEnd]

			if reExposeAll.MatchString(closure) {
				for _, op := range []string{"list", "show", "create", "update", "delete"} {
					exposures[model][op] = true
				}
			} else {
				for _, opMatch := range reExposeOps.FindAllStringSubmatch(closure, -1) {
					exposures[model][strings.ToLower(opMatch[1])] = true
				}
			}
		}

		// Process AlterExpose calls
		for _, match := range reAlterExposeDir.FindAllStringSubmatchIndex(src, -1) {
			model := src[match[2]:match[3]]
			if exposures[model] == nil {
				exposures[model] = map[string]bool{}
				modelOrder = append(modelOrder, model)
			}
			closureEnd := findClosureEnd(src, match[1])
			closure := src[match[1]:closureEnd]

			// Add ops
			for _, opMatch := range reExposeOps.FindAllStringSubmatch(closure, -1) {
				exposures[model][strings.ToLower(opMatch[1])] = true
			}
			// Remove ops
			for _, opMatch := range reRemoveOps.FindAllStringSubmatch(closure, -1) {
				op := strings.ToLower(strings.TrimPrefix(opMatch[1], "Remove"))
				delete(exposures[model], op)
			}
		}

		// Process Unexpose calls
		for _, match := range reUnexposeDir.FindAllStringSubmatch(src, -1) {
			model := match[1]
			delete(exposures, model)
			for i, m := range modelOrder {
				if m == model {
					modelOrder = append(modelOrder[:i], modelOrder[i+1:]...)
					break
				}
			}
		}

		// Process ControllerAction calls
		for _, match := range reControllerAction.FindAllStringSubmatch(src, -1) {
			actions[match[1]] = true
		}

		// Process RemoveAction calls
		for _, match := range reRemoveAction.FindAllStringSubmatch(src, -1) {
			delete(actions, match[1])
		}
	}

	var state DerivedGraphQLState
	for _, model := range modelOrder {
		if ops, ok := exposures[model]; ok && len(ops) > 0 {
			var opList []string
			for _, o := range []string{"list", "show", "create", "update", "delete"} {
				if ops[o] {
					opList = append(opList, o)
				}
			}
			state.Exposures = append(state.Exposures, DerivedExposure{
				Model:      model,
				Operations: opList,
			})
		}
	}
	for name := range actions {
		state.Actions = append(state.Actions, DerivedAction{Name: name})
	}
	sort.Slice(state.Actions, func(i, j int) bool {
		return state.Actions[i].Name < state.Actions[j].Name
	})
	return state
}

// findClosureEnd finds the matching closing brace after a position in src.
func findClosureEnd(src string, start int) int {
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}
