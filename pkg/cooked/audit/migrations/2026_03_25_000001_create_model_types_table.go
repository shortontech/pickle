//go:build ignore

package migrations

// CreateModelTypesTable_2026_03_25_000001 creates the model_types table for audit trails.
type CreateModelTypesTable_2026_03_25_000001 struct {
	Migration
}

func (m *CreateModelTypesTable_2026_03_25_000001) Up() {
	m.CreateTable("model_types", func(t *Table) {
		t.AppendOnly()
		t.Integer("id").PrimaryKey()
		t.String("name", 100).NotNull().Unique()
	})
}

func (m *CreateModelTypesTable_2026_03_25_000001) Down() {
	m.DropTableIfExists("model_types")
}
