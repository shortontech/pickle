package squeeze

import (
	"testing"

	"github.com/shortontech/pickle/pkg/schema"
)

func TestRuleRequiredFields_FlagsMissingRequiredField(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
	}
	models.QueryPost().Create(post)
}`
	m := method(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "body", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	// body is required but not set
	found := false
	for _, f := range findings {
		if f.Rule == "required_fields" {
			found = true
		}
	}
	if !found {
		t.Error("expected required_fields finding for missing Body field")
	}
}

func TestRuleRequiredFields_PassesAllRequiredFieldsSet(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
		Body:  "world",
	}
	models.QueryPost().Create(post)
}`
	m := method(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "body", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d: %v", len(findings), findings)
	}
}

func TestRuleRequiredFields_SkipsNullableColumns(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
	}
	models.QueryPost().Create(post)
}`
	m := method(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "description", IsNullable: true, HasDefault: false, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for nullable column, got %d", len(findings))
	}
}

func TestRuleRequiredFields_SkipsColumnsWithDefault(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
	}
	models.QueryPost().Create(post)
}`
	m := method(t, src)

	defaultVal := "pending"
	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "status", IsNullable: false, HasDefault: true, DefaultValue: &defaultVal, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings for column with default, got %d", len(findings))
	}
}

func TestRuleRequiredFields_SkipsPrimaryKey(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
	}
	models.QueryPost().Create(post)
}`
	m := method(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "id", IsNullable: false, HasDefault: false, IsPrimaryKey: true},
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (id is PK), got %d", len(findings))
	}
}

func TestRuleRequiredFields_SkipsTimestamps(t *testing.T) {
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
	}
	models.QueryPost().Create(post)
}`
	m := method(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "created_at", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "updated_at", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings (timestamps are skipped), got %d", len(findings))
	}
}

func TestRuleRequiredFields_SkipsNoCreate(t *testing.T) {
	// Model literal exists but there's no .Create() call → should not flag
	src := `package controllers
import "models"
func Handler() {
	post := &models.Post{
		Title: "hello",
	}
	_ = post
}`
	m := method(t, src)

	ctx := &AnalysisContext{
		Methods: map[string]*ControllerMethod{
			"PostController.Store": m,
		},
		Tables: []*schema.Table{
			{
				Name: "posts",
				Columns: []*schema.Column{
					{Name: "title", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
					{Name: "body", IsNullable: false, HasDefault: false, IsPrimaryKey: false},
				},
			},
		},
	}

	findings := ruleRequiredFields(ctx)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings without Create() call, got %d", len(findings))
	}
}
