package main

import "testing"

func TestHelpRequested(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"-h"},
		{"--project", "/tmp/dill", "--help"},
	} {
		if !helpRequested(args) {
			t.Errorf("helpRequested(%q) = false, want true", args)
		}
	}

	if helpRequested([]string{"CRMSeeder", "--dry-run"}) {
		t.Error("ordinary db:seed arguments requested help")
	}
}
