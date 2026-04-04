package generator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNonManagesRoleAnnotations(t *testing.T) {
	dir := t.TempDir()

	src := `package policies

type CreateRoles_2026_01_01_000000 struct{ Policy }

func (m *CreateRoles_2026_01_01_000000) Up() {
	m.CreateRole("admin").Name("Administrator").Manages()
	m.CreateRole("compliance").Name("Compliance")
	m.CreateRole("support_lead").Name("Support Lead")
}

func (m *CreateRoles_2026_01_01_000000) Down() {
	m.DropRole("support_lead")
	m.DropRole("compliance")
	m.DropRole("admin")
}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_01_01_000000_create_roles.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	annotations, err := NonManagesRoleAnnotations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 2 {
		t.Fatalf("expected 2 annotations (admin excluded), got %d", len(annotations))
	}
	if annotations[0].Slug != "compliance" || annotations[0].PascalName != "Compliance" {
		t.Errorf("unexpected first annotation: %+v", annotations[0])
	}
	if annotations[1].Slug != "support_lead" || annotations[1].PascalName != "SupportLead" {
		t.Errorf("unexpected second annotation: %+v", annotations[1])
	}
}

func TestNonManagesRoleAnnotationsNoDir(t *testing.T) {
	annotations, err := NonManagesRoleAnnotations("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if annotations != nil {
		t.Error("expected nil for nonexistent dir")
	}
}

func TestNonManagesRoleAnnotationsDroppedRole(t *testing.T) {
	dir := t.TempDir()

	p1 := `package policies

type P1_2026_01_01_000000 struct{ Policy }

func (m *P1_2026_01_01_000000) Up() {
	m.CreateRole("admin").Manages()
	m.CreateRole("reviewer")
	m.CreateRole("viewer")
}

func (m *P1_2026_01_01_000000) Down() {}
`
	p2 := `package policies

type P2_2026_01_02_000000 struct{ Policy }

func (m *P2_2026_01_02_000000) Up() {
	m.DropRole("reviewer")
}

func (m *P2_2026_01_02_000000) Down() {}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_01_01_000000_p1.go"), []byte(p1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026_01_02_000000_p2.go"), []byte(p2), 0o644); err != nil {
		t.Fatal(err)
	}

	annotations, err := NonManagesRoleAnnotations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 1 {
		t.Fatalf("expected 1 annotation (reviewer dropped, admin manages), got %d", len(annotations))
	}
	if annotations[0].Slug != "viewer" {
		t.Errorf("expected viewer, got %s", annotations[0].Slug)
	}
}

func TestNonManagesRoleAnnotationsMultipleRoles(t *testing.T) {
	dir := t.TempDir()

	src := `package policies

type P_2026_01_01_000000 struct{ Policy }

func (m *P_2026_01_01_000000) Up() {
	m.CreateRole("admin").Manages()
	m.CreateRole("editor")
	m.CreateRole("viewer").Default()
	m.CreateRole("auditor")
}

func (m *P_2026_01_01_000000) Down() {}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_01_01_000000_p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	annotations, err := NonManagesRoleAnnotations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 3 {
		t.Fatalf("expected 3 non-manages annotations, got %d", len(annotations))
	}
	slugs := make([]string, len(annotations))
	for i, a := range annotations {
		slugs[i] = a.Slug
	}
	expected := []string{"editor", "viewer", "auditor"}
	for i, s := range expected {
		if slugs[i] != s {
			t.Errorf("annotation[%d]: expected %s, got %s", i, s, slugs[i])
		}
	}
}

func TestNonManagesRoleAnnotationsAddingRole(t *testing.T) {
	dir := t.TempDir()

	p1 := `package policies

type P1_2026_01_01_000000 struct{ Policy }

func (m *P1_2026_01_01_000000) Up() {
	m.CreateRole("admin").Manages()
	m.CreateRole("editor")
}

func (m *P1_2026_01_01_000000) Down() {}
`
	p2 := `package policies

type P2_2026_01_02_000000 struct{ Policy }

func (m *P2_2026_01_02_000000) Up() {
	m.CreateRole("auditor")
}

func (m *P2_2026_01_02_000000) Down() {}
`
	if err := os.WriteFile(filepath.Join(dir, "2026_01_01_000000_p1.go"), []byte(p1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026_01_02_000000_p2.go"), []byte(p2), 0o644); err != nil {
		t.Fatal(err)
	}

	annotations, err := NonManagesRoleAnnotations(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(annotations) != 2 {
		t.Fatalf("expected 2 annotations, got %d", len(annotations))
	}
	if annotations[0].Slug != "editor" {
		t.Errorf("expected editor first, got %s", annotations[0].Slug)
	}
	if annotations[1].Slug != "auditor" {
		t.Errorf("expected auditor second, got %s", annotations[1].Slug)
	}
}
