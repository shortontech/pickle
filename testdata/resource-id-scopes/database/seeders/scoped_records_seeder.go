package seeders

import seed "example.com/resource-id-scopes/database/migrations"

var (
	OrganizationSeeder = seed.NewRowSeederRef("OrganizationSeeder", "organizations")
	RecordSeeder       = seed.NewRowSeederRef("RecordSeeder", "records")
	RecordNoteSeeder   = seed.NewRowSeederRef("RecordNoteSeeder", "record_notes")
)

type ScopedRecordsSeeder struct{ seed.ScenarioSeeder }

func (ScopedRecordsSeeder) Seed(graph *seed.SeedGraph) {
	organization := graph.Create(OrganizationSeeder).One()
	records := graph.CreateN(RecordSeeder, 25).For(organization, "organization_id").Many()
	graph.ForEach(records, func(record seed.SeedRecord) {
		graph.CreateN(RecordNoteSeeder, graph.Between(1, 8)).For(record)
	})
}
