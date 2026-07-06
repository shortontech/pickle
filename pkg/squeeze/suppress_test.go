package squeeze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSuppressionDirective(t *testing.T) {
	tests := []struct {
		comment  string
		wantRule string
		wantOK   bool
	}{
		{"//squeeze:ignore read_scoping", "read_scoping", true},
		{"//squeeze:ignore read_scoping self read is safe", "read_scoping", true},
		{"// squeeze:ignore unbounded_query", "unbounded_query", true},
		{"//squeeze:ignore", "", false},             // no rule named — not a valid directive
		{"// just a comment", "", false},            // unrelated comment
		{"//squeeze:ignoreread_scoping", "", false}, // must be the exact token
	}
	for _, tt := range tests {
		rule, ok := parseSuppressionDirective(tt.comment)
		if ok != tt.wantOK || rule != tt.wantRule {
			t.Errorf("parseSuppressionDirective(%q) = (%q, %v), want (%q, %v)", tt.comment, rule, ok, tt.wantRule, tt.wantOK)
		}
	}
}

func TestCollectSuppressions_FromDecl(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "user_controller.go")
	src := `package controllers

//squeeze:ignore read_scoping self read is safe
func Session() {
	x := 1
	_ = x
}

func Other() {
	y := 1
	_ = y
}
`
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	sups := collectSuppressions([]string{file}, "")
	if len(sups) != 1 {
		t.Fatalf("expected 1 suppression, got %d", len(sups))
	}
	if sups[0].Rule != "read_scoping" {
		t.Errorf("rule = %q, want read_scoping", sups[0].Rule)
	}
	if sups[0].File != file {
		t.Errorf("file = %q, want %q", sups[0].File, file)
	}
	// func Session spans lines 4-7.
	if sups[0].Start != 4 || sups[0].End != 7 {
		t.Errorf("range = [%d,%d], want [4,7]", sups[0].Start, sups[0].End)
	}
}

// The directive suppresses exactly the named rule on the annotated declaration and
// nothing else (acceptance criterion 3).
func TestApplySuppressions_ScopeAndRule(t *testing.T) {
	sups := []suppression{
		{Rule: "read_scoping", File: "ctrl.go", Start: 4, End: 7},
	}
	findings := []Finding{
		{Rule: "read_scoping", File: "ctrl.go", Line: 4, Message: "in-range, matching rule -> suppressed"},
		{Rule: "read_scoping", File: "ctrl.go", Line: 99, Message: "out of range -> kept"},
		{Rule: "unbounded_query", File: "ctrl.go", Line: 4, Message: "different rule -> kept"},
		{Rule: "read_scoping", File: "other.go", Line: 4, Message: "different file -> kept"},
	}
	kept, suppressed := applySuppressions(findings, sups)
	if len(suppressed) != 1 {
		t.Fatalf("expected 1 suppressed, got %d", len(suppressed))
	}
	if suppressed[0].Message != "in-range, matching rule -> suppressed" {
		t.Errorf("wrong finding suppressed: %q", suppressed[0].Message)
	}
	if len(kept) != 3 {
		t.Errorf("expected 3 kept findings, got %d", len(kept))
	}
}

// End-to-end: a real finding inside a declaration carrying the directive is
// removed, and the run surfaces the suppressed count.
func TestSuppression_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "post_controller.go")
	src := `package controllers

//squeeze:ignore read_scoping intentional global feed
func Index() {
	x := 1
	_ = x
}
`
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// A finding pointing at the func declaration line (4).
	findings := []Finding{
		{Rule: "read_scoping", File: file, Line: 4, Message: "possible IDOR"},
	}
	sups := collectSuppressions(uniqueFindingFiles(findings), "")
	kept, suppressed := applySuppressions(findings, sups)
	if len(kept) != 0 {
		t.Errorf("expected finding suppressed, got %d kept", len(kept))
	}
	if len(suppressed) != 1 {
		t.Errorf("expected suppressed count 1, got %d", len(suppressed))
	}
}
