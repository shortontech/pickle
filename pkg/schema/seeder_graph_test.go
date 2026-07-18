package schema

import "testing"

func TestSeedNodeRepeatIdentity(t *testing.T) {
	graph := &SeedGraph{}
	graph.Create(NewRowSeederRef("UserSeeder", "users")).UniqueBy("email").Update("name")
	node := graph.Nodes[0]
	if len(node.UniqueColumns) != 1 || node.UniqueColumns[0] != "email" {
		t.Fatalf("unique identity = %#v", node.UniqueColumns)
	}
	if len(node.UpdateColumns) != 1 || node.UpdateColumns[0] != "name" {
		t.Fatalf("update allowlist = %#v", node.UpdateColumns)
	}
}

func TestSeedNodeRepeatIdentityRejectsDuplicates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate identity panic")
		}
	}()
	graph := &SeedGraph{}
	graph.Create(NewRowSeederRef("UserSeeder", "users")).UniqueBy("email", "email")
}

func TestSeedGraphFactoriesDependenciesAndThrough(t *testing.T) {
	graph := &SeedGraph{}
	catalog := graph.DependsOn(NewRowSeederRef("RoleCatalogSeeder", "roles"))
	user := graph.Create(NewRowSeederRef("UserSeeder", "users")).
		DependsOn(catalog).
		WithFactory("email", NewSeederRef("WorkEmailSeeder", String)).
		One()
	graph.Create(NewRowSeederRef("ContactSeeder", "contacts")).For(user, Through("user_id"))
	if len(graph.Nodes) != 3 || graph.Nodes[1].Dependencies[0] != catalog.NodeID || graph.Nodes[1].Factories["email"].Name != "WorkEmailSeeder" || graph.Nodes[2].Through != "user_id" {
		t.Fatalf("graph = %#v", graph.Nodes)
	}
}
