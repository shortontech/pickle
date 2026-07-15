package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/shortontech/pickle/pkg/generator"
	"github.com/shortontech/pickle/pkg/schema"
)

func resourceIDMethod(t *testing.T, body string) *ControllerMethod {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "controller.go", "package controllers\nfunc (c C) Show(ctx *Context) {\n"+body+"\n}", 0)
	if err != nil {
		t.Fatal(err)
	}
	fn := file.Decls[0].(*ast.FuncDecl)
	return &ControllerMethod{ControllerType: "C", MethodName: "Show", File: "controller.go", Body: fn.Body, Fset: fset}
}

func TestResourceIDUUIDParserFlagsProvenRouteValue(t *testing.T) {
	method := resourceIDMethod(t, `id, err := ctx.ParamResourceID("party_id"); _, _ = uuid.Parse(id.String()); _, _ = err, id`)
	findings := ruleResourceIDUUIDParser(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDUUIDParserFlagsTypedRequestField(t *testing.T) {
	method := resourceIDMethod(t, `req, err := requests.BindLoadPartyRequest(ctx.Request()); _, _ = uuid.Parse(req.PartyID.String()); _ = err`)
	requests := []generator.RequestDef{{Name: "LoadPartyRequest", Fields: []generator.RequestField{{Name: "PartyID", IsResourceID: true}}}}
	findings := ruleResourceIDUUIDParser(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}, Requests: requests})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDUUIDParserIgnoresOrdinaryUUIDAndNames(t *testing.T) {
	method := resourceIDMethod(t, `id, _ := uuid.Parse(ctx.Param("id")); resourceID := "not typed"; _, _ = uuid.Parse(resourceID); _ = id`)
	findings := ruleResourceIDUUIDParser(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}})
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDUUIDParserFlagsConflictingParamParser(t *testing.T) {
	method := resourceIDMethod(t, `_, _ = ctx.ParamResourceIDParts("party_id"); _, _ = ctx.ParamUUID("party_id")`)
	findings := ruleResourceIDUUIDParser(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDUnscopedFlagsProvenRecordOnlyQuery(t *testing.T) {
	method := resourceIDMethod(t, `parts, _ := ctx.ParamResourceIDParts("record_id"); _, _ = models.QueryRecord().WhereRecordID(parts.RecordID).First()`)
	tables := []*schema.Table{{Name: "records", CompositePrimaryKeys: []string{"organization_id", "record_id"}}}
	findings := ruleResourceIDUnscoped(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}, Tables: tables})
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDUnscopedAcceptsScopeAndRecordQuery(t *testing.T) {
	method := resourceIDMethod(t, `parts, _ := ctx.ParamResourceIDParts("record_id"); _, _ = models.QueryRecord().WhereOrganizationID(parts.ScopeID).WhereRecordID(parts.RecordID).First()`)
	tables := []*schema.Table{{Name: "records", CompositePrimaryKeys: []string{"organization_id", "record_id"}}}
	findings := ruleResourceIDUnscoped(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}, Tables: tables})
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDUnscopedIgnoresAliasesAndUnprovenNames(t *testing.T) {
	method := resourceIDMethod(t, `parts := struct{ RecordID int64 }{RecordID: 1}; _, _ = models.QueryRecord().WhereRecordID(parts.RecordID).First()`)
	tables := []*schema.Table{{Name: "records", CompositePrimaryKeys: []string{"organization_id", "record_id"}}}
	findings := ruleResourceIDUnscoped(&AnalysisContext{Methods: map[string]*ControllerMethod{"C.Show": method}, Tables: tables})
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestResourceIDScopeFixtureRejectsOnlyUnsafeLoad(t *testing.T) {
	methods, err := ParseControllers("../../testdata/resource-id-scopes/app/http/controllers")
	if err != nil {
		t.Fatal(err)
	}
	tables := []*schema.Table{{Name: "records", CompositePrimaryKeys: []string{"organization_id", "record_id"}}}
	findings := ruleResourceIDUnscoped(&AnalysisContext{Methods: methods, Tables: tables})
	if len(findings) != 1 || findings[0].Rule != "resource_id_unscoped" {
		t.Fatalf("findings = %#v", findings)
	}
}
