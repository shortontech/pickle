//go:build ignore

package migrations

// CreateActionTypesTable_2026_03_25_000002 creates the action_types table for audit trails.
type CreateActionTypesTable_2026_03_25_000002 struct {
	Migration
}

func (m *CreateActionTypesTable_2026_03_25_000002) Up() {
	m.CreateTable("action_types", func(t *Table) {
		t.AppendOnly()
		t.Integer("id").PrimaryKey()
		t.Integer("model_type_id").NotNull().ForeignKey("model_types", "id")
		t.String("name", 100).NotNull()
	})

	m.AddIndex("action_types", "model_type_id")
	m.AddUniqueIndex("action_types", "model_type_id", "name")
}

func (m *CreateActionTypesTable_2026_03_25_000002) Down() {
	m.DropTableIfExists("action_types")
}
