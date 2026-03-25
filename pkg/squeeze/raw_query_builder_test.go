package squeeze

import (
	"testing"
)

func TestRawQueryBuilderAccess_FlagsOrderBy(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.QueryBuilder.OrderBy("email", "ASC")
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "raw_query_builder_access" {
		t.Errorf("rule = %q", findings[0].Rule)
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("severity = %v, want warning", findings[0].Severity)
	}
}

func TestRawQueryBuilderAccess_FlagsWhere(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.QueryBuilder.Where("email", email)
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRawQueryBuilderAccess_FlagsWhereIn(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.QueryBuilder.WhereIn("status", statuses)
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRawQueryBuilderAccess_FlagsWhereNotIn(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.QueryBuilder.WhereNotIn("status", statuses)
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestRawQueryBuilderAccess_FlagsImmutableQueryBuilder(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.ImmutableQueryBuilder.OrderBy("email", "ASC")
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Message != "direct ImmutableQueryBuilder.OrderBy() bypasses typed query API — use the generated OrderBy{Column}/Where{Column} methods instead" {
		t.Errorf("unexpected message: %s", findings[0].Message)
	}
}

func TestRawQueryBuilderAccess_IgnoresTypedMethods(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.WhereEmail("test@example.com")
	q.OrderByCreatedAt("ASC")
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestRawQueryBuilderAccess_IgnoresTerminalMethods(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.QueryBuilder.First()
	q.QueryBuilder.All()
	q.QueryBuilder.Count()
	q.QueryBuilder.Create(u)
	q.QueryBuilder.Update(u)
	q.QueryBuilder.Delete(u)
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for terminal methods, got %d", len(findings))
	}
}

func TestRawQueryBuilderAccess_MultipleFindings(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	q := models.QueryUser()
	q.QueryBuilder.OrderBy("email", "ASC")
	q.QueryBuilder.Where("status", "active")
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
}

func TestRawQueryBuilderAccess_ChainedCall(t *testing.T) {
	m := method(t, `package controllers
func Index() {
	models.QueryUser().QueryBuilder.OrderBy("email", "ASC")
}
`)
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{"UserController.Index": m},
	}
	findings := ruleRawQueryBuilderAccess(ctx)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}
