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
	Model         string
	Operations    []string // "list", "show", "create", "update", "delete"
	Relationships []DerivedRelationshipExposure
}

// DerivedAction represents a registered custom controller action.
type DerivedAction struct {
	Name string
}

// DerivedRelationshipExposure is optional relationship budget metadata derived
// from GraphQL exposure policies.
type DerivedRelationshipExposure struct {
	Name        string
	Cost        int
	MaxPageSize int
}

// ExposedModel pairs a model with its table definition and the GraphQL
// operations that should be generated for it.
type ExposedModel struct {
	Model         string
	Table         *schema.Table
	Operations    []string
	Relationships []DerivedRelationshipExposure
}

// GraphQLModelPlan carries the exposed table plus the exact operations that
// generation is allowed to emit for it.
type GraphQLModelPlan struct {
	Table         *schema.Table
	Operations    map[string]bool
	Relationships map[string]DerivedRelationshipExposure
}

func legacyGraphQLModelPlans(tables []*schema.Table) []GraphQLModelPlan {
	var plans []GraphQLModelPlan
	for _, tbl := range tables {
		plans = append(plans, GraphQLModelPlan{
			Table:         tbl,
			Relationships: map[string]DerivedRelationshipExposure{},
			Operations: map[string]bool{
				"list":   true,
				"show":   true,
				"create": true,
				"update": true,
				"delete": true,
			},
		})
	}
	return plans
}

func exposedGraphQLModelPlans(models []*ExposedModel) []GraphQLModelPlan {
	var plans []GraphQLModelPlan
	for _, model := range models {
		ops := map[string]bool{}
		for _, op := range model.Operations {
			ops[op] = true
		}
		rels := map[string]DerivedRelationshipExposure{}
		for _, rel := range model.Relationships {
			rels[rel.Name] = rel
		}
		plans = append(plans, GraphQLModelPlan{
			Table:         model.Table,
			Operations:    ops,
			Relationships: rels,
		})
	}
	return plans
}

func operationAllowed(plan GraphQLModelPlan, op string) bool {
	return plan.Operations != nil && plan.Operations[op]
}

func planForTable(plans []GraphQLModelPlan, tableName string) (GraphQLModelPlan, bool) {
	for _, plan := range plans {
		if plan.Table != nil && plan.Table.Name == tableName {
			return plan, true
		}
	}
	return GraphQLModelPlan{}, false
}

func tableSetFromPlans(plans []GraphQLModelPlan) map[string]bool {
	set := map[string]bool{}
	for _, plan := range plans {
		if plan.Table != nil {
			set[plan.Table.Name] = true
		}
	}
	return set
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
			Model:         exp.Model,
			Table:         tbl,
			Operations:    exp.Operations,
			Relationships: exp.Relationships,
		})
	}
	return result
}

var (
	reExposeDir        = regexp.MustCompile(`(?m)\.Expose\("([^"]+)"`)
	reAlterExposeDir   = regexp.MustCompile(`(?m)\.AlterExpose\("([^"]+)"`)
	reUnexposeDir      = regexp.MustCompile(`(?m)\.Unexpose\("([^"]+)"\)`)
	reControllerAction = regexp.MustCompile(`(?m)\.ControllerAction\("([^"]+)"`)
	reRemoveAction     = regexp.MustCompile(`(?m)\.RemoveAction\("([^"]+)"\)`)
	reExposeOps        = regexp.MustCompile(`e\.(List|Show|Create|Update|Delete)\(\)`)
	reRemoveOps        = regexp.MustCompile(`e\.(RemoveList|RemoveShow|RemoveCreate|RemoveUpdate|RemoveDelete)\(\)`)
	reExposeAll        = regexp.MustCompile(`e\.All\(\)`)
	reUpMethod         = regexp.MustCompile(`func\s*\([^)]*\)\s*Up\s*\(\)\s*\{`)
	reRelationship     = regexp.MustCompile(`e\.Relationship\("([^"]+)"`)
	reRelationshipCost = regexp.MustCompile(`r\.Cost\(([0-9]+)\)`)
	reRelationshipMax  = regexp.MustCompile(`r\.MaxPageSize\(([0-9]+)\)`)
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
	relationships := map[string]map[string]DerivedRelationshipExposure{}
	actions := map[string]bool{}
	var modelOrder []string

	for _, fname := range files {
		data, err := os.ReadFile(filepath.Join(dir, fname))
		if err != nil {
			continue
		}
		src := upMethodBody(string(data))
		if src == "" {
			continue
		}

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
			collectRelationshipExposures(model, closure, relationships)
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
			collectRelationshipExposures(model, closure, relationships)
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
			exp := DerivedExposure{Model: model, Operations: opList}
			for _, rel := range relationships[model] {
				exp.Relationships = append(exp.Relationships, rel)
			}
			sort.Slice(exp.Relationships, func(i, j int) bool {
				return exp.Relationships[i].Name < exp.Relationships[j].Name
			})
			state.Exposures = append(state.Exposures, exp)
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

func collectRelationshipExposures(model, closure string, relationships map[string]map[string]DerivedRelationshipExposure) {
	for _, match := range reRelationship.FindAllStringSubmatchIndex(closure, -1) {
		name := closure[match[2]:match[3]]
		relClosureEnd := findClosureEnd(closure, match[1])
		relClosure := closure[match[1]:relClosureEnd]
		rel := DerivedRelationshipExposure{Name: name}
		if cost := reRelationshipCost.FindStringSubmatch(relClosure); len(cost) == 2 {
			rel.Cost = parsePositiveDecimal(cost[1])
		}
		if max := reRelationshipMax.FindStringSubmatch(relClosure); len(max) == 2 {
			rel.MaxPageSize = parsePositiveDecimal(max[1])
		}
		if relationships[model] == nil {
			relationships[model] = map[string]DerivedRelationshipExposure{}
		}
		relationships[model][name] = rel
	}
}

func parsePositiveDecimal(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func upMethodBody(src string) string {
	match := reUpMethod.FindStringIndex(src)
	if match == nil {
		return ""
	}
	end := findClosureEnd(src, match[1]-1)
	return src[match[1]:end]
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
