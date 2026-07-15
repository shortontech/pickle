package generator

import (
	"strings"
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestValidateSeedGraphResolvesCompositeRelationship(t *testing.T) {
	organizations := &schema.Table{Name: "organizations", Columns: []*schema.Column{{Name: "organization_id", IsPrimaryKey: true}, {Name: "contact_id", IsPrimaryKey: true}}}
	contacts := &schema.Table{Name: "contacts", Columns: []*schema.Column{{Name: "organization_id"}, {Name: "contact_id"}}, ForeignKeys: []*schema.ForeignKey{{Columns: []string{"organization_id", "contact_id"}, ReferencedTable: "organizations", ReferencedColumns: []string{"organization_id", "contact_id"}}}}
	graph := &schema.SeedGraph{}
	parent := graph.Create(schema.NewRowSeederRef("OrganizationSeeder", "organizations")).One()
	graph.CreateN(schema.NewRowSeederRef("ContactSeeder", "contacts"), 25).For(parent)
	rels, err := ValidateSeedGraph(graph, []*schema.Table{organizations, contacts})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || strings.Join(rels[0].Columns, ",") != "organization_id,contact_id" {
		t.Fatalf("relationships = %#v", rels)
	}
}

func TestValidateSeedGraphRejectsAmbiguousRelationship(t *testing.T) {
	users := &schema.Table{Name: "users"}
	contacts := &schema.Table{Name: "contacts", Columns: []*schema.Column{
		{Name: "owner_id", ForeignKeyTable: "users", ForeignKeyColumn: "id"},
		{Name: "creator_id", ForeignKeyTable: "users", ForeignKeyColumn: "id"},
	}}
	graph := &schema.SeedGraph{}
	parent := graph.Create(schema.NewRowSeederRef("UserSeeder", "users")).One()
	graph.Create(schema.NewRowSeederRef("ContactSeeder", "contacts")).For(parent)
	_, err := ValidateSeedGraph(graph, []*schema.Table{users, contacts})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestSeedGraphNestedCountsAndOverrides(t *testing.T) {
	graph := &schema.SeedGraph{}
	user := graph.Create(schema.NewRowSeederRef("UserSeeder", "users")).With("name", "Ada").One()
	contacts := graph.CreateN(schema.NewRowSeederRef("ContactSeeder", "contacts"), 25).For(user, "user_id").Many()
	graph.ForEach(contacts, func(contact schema.SeedRecord) {
		graph.CreateN(schema.NewRowSeederRef("NoteSeeder", "notes"), graph.Between(1, 8)).For(contact)
	})
	if len(graph.Nodes) != 3 || graph.Nodes[2].Count.Min != 1 || graph.Nodes[2].Count.Max != 8 {
		t.Fatalf("nodes = %#v", graph.Nodes)
	}
}
