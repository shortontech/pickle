package generator

import "github.com/shortontech/pickle/pkg/schema"

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
