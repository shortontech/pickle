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
