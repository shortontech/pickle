package squeeze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// AnalyzedRoute represents a single route extracted from routes/*.go via AST.
type AnalyzedRoute struct {
	Method         string   // GET, POST, PUT, PATCH, DELETE
	Path           string   // full path including group prefixes
	ControllerType string   // e.g. "PostController"
	MethodName     string   // e.g. "Destroy"
	Middleware     []string // accumulated middleware names from groups + per-route
	File           string
	Line           int
}

// HasAuthMiddleware returns true if any middleware on this route is classified as auth.
func (r AnalyzedRoute) HasAuthMiddleware(mc MiddlewareConfig) bool {
	for _, mw := range r.Middleware {
		if mc.IsAuthMiddleware(mw) {
			return true
		}
	}
	return false
}

// HasAdminMiddleware returns true if any middleware on this route is classified as admin.
func (r AnalyzedRoute) HasAdminMiddleware(mc MiddlewareConfig) bool {
	for _, mw := range r.Middleware {
		if mc.IsAdminMiddleware(mw) {
			return true
		}
	}
	return false
}

var httpMethods = map[string]string{
	"Get":    "GET",
	"Post":   "POST",
	"Put":    "PUT",
	"Patch":  "PATCH",
	"Delete": "DELETE",
}

// ParseRoutes parses all Go files in the routes directory and extracts route definitions.
func ParseRoutes(routesDir string) ([]AnalyzedRoute, error) {
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var routes []AnalyzedRoute

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") || strings.HasSuffix(e.Name(), "_gen.go") {
			continue
		}

		path := filepath.Join(routesDir, e.Name())
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}

		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}

			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 {
					continue
				}

				// Look for pickle.Routes(func(r *pickle.Router) { ... })
				call, ok := vs.Values[0].(*ast.CallExpr)
				if !ok {
					continue
				}

				if !isRoutesCall(call) {
					continue
				}

				// The first (and only) argument should be a func literal
				if len(call.Args) < 1 {
					continue
				}
				fn, ok := call.Args[0].(*ast.FuncLit)
				if !ok {
					continue
				}

				routerParam := extractRouterParamName(fn)
				parsed := walkRouterBody(fn.Body, routerParam, "", nil, fset, path)
				routes = append(routes, parsed...)
			}
		}
	}

	return routes, nil
}

// isRoutesCall checks if a call expression is pickle.Routes(...) or Routes(...)
func isRoutesCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "Routes"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "Routes"
	}
	return false
}

// extractRouterParamName gets the parameter name from func(r *pickle.Router)
func extractRouterParamName(fn *ast.FuncLit) string {
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return "r"
	}
	param := fn.Type.Params.List[0]
	if len(param.Names) > 0 {
		return param.Names[0].Name
	}
	return "r"
}

// walkRouterBody recursively walks a router function body extracting routes.
func walkRouterBody(body *ast.BlockStmt, routerName, prefix string, parentMW []string, fset *token.FileSet, file string) []AnalyzedRoute {
	var routes []AnalyzedRoute

	for _, stmt := range body.List {
		exprStmt, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := exprStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}

		// Verify it's called on the router variable
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != routerName {
			continue
		}

		methodName := sel.Sel.Name

		if methodName == "Group" {
			routes = append(routes, parseGroup(call, routerName, prefix, parentMW, fset, file)...)
		} else if methodName == "Resource" {
			routes = append(routes, parseResource(call, prefix, parentMW, fset, file)...)
		} else if _, ok := httpMethods[methodName]; ok {
			if r, ok := parseRoute(methodName, call, prefix, parentMW, fset, file); ok {
				routes = append(routes, r)
			}
		}
	}

	return routes
}

// parseGroup handles r.Group("/prefix", func(r *Router) { ... }, middleware...)
// Signature: Group(prefix string, body func(*Router), mw ...MiddlewareFunc)
func parseGroup(call *ast.CallExpr, routerName, parentPrefix string, parentMW []string, fset *token.FileSet, file string) []AnalyzedRoute {
	if len(call.Args) < 2 {
		return nil
	}

	groupPrefix := extractStringLit(call.Args[0])

	// Find the func literal (body) and collect middleware from remaining args
	var body *ast.FuncLit
	var mwNames []string

	for _, arg := range call.Args[1:] {
		if fn, ok := arg.(*ast.FuncLit); ok {
			body = fn
		} else {
			if name := extractMiddlewareName(arg); name != "" {
				mwNames = append(mwNames, name)
			}
		}
	}

	if body == nil {
		return nil
	}

	// Accumulate middleware from parent groups
	allMW := make([]string, 0, len(parentMW)+len(mwNames))
	allMW = append(allMW, parentMW...)
	allMW = append(allMW, mwNames...)

	childRouter := extractRouterParamName(body)
	fullPrefix := parentPrefix + groupPrefix

	return walkRouterBody(body.Body, childRouter, fullPrefix, allMW, fset, file)
}

// parseResource handles r.Resource("/path", controller{}, middleware...)
// Expands to Index, Show, Store, Update, Destroy
func parseResource(call *ast.CallExpr, parentPrefix string, parentMW []string, fset *token.FileSet, file string) []AnalyzedRoute {
	if len(call.Args) < 2 {
		return nil
	}

	resourcePath := extractStringLit(call.Args[0])
	ctrlType := extractControllerType(call.Args[1])

	var mwNames []string
	for _, arg := range call.Args[2:] {
		if name := extractMiddlewareName(arg); name != "" {
			mwNames = append(mwNames, name)
		}
	}

	allMW := make([]string, 0, len(parentMW)+len(mwNames))
	allMW = append(allMW, parentMW...)
	allMW = append(allMW, mwNames...)

	fullPath := parentPrefix + resourcePath
	line := fset.Position(call.Pos()).Line

	return []AnalyzedRoute{
		{Method: "GET", Path: fullPath, ControllerType: ctrlType, MethodName: "Index", Middleware: allMW, File: file, Line: line},
		{Method: "GET", Path: fullPath + "/:id", ControllerType: ctrlType, MethodName: "Show", Middleware: allMW, File: file, Line: line},
		{Method: "POST", Path: fullPath, ControllerType: ctrlType, MethodName: "Store", Middleware: allMW, File: file, Line: line},
		{Method: "PUT", Path: fullPath + "/:id", ControllerType: ctrlType, MethodName: "Update", Middleware: allMW, File: file, Line: line},
		{Method: "DELETE", Path: fullPath + "/:id", ControllerType: ctrlType, MethodName: "Destroy", Middleware: allMW, File: file, Line: line},
	}
}

// parseRoute handles r.Get("/path", handler, middleware...)
func parseRoute(method string, call *ast.CallExpr, parentPrefix string, parentMW []string, fset *token.FileSet, file string) (AnalyzedRoute, bool) {
	if len(call.Args) < 2 {
		return AnalyzedRoute{}, false
	}

	routePath := extractStringLit(call.Args[0])
	ctrlType, methodName := extractHandlerRef(call.Args[1])

	var mwNames []string
	for _, arg := range call.Args[2:] {
		if name := extractMiddlewareName(arg); name != "" {
			mwNames = append(mwNames, name)
		}
	}

	allMW := make([]string, 0, len(parentMW)+len(mwNames))
	allMW = append(allMW, parentMW...)
	allMW = append(allMW, mwNames...)

	return AnalyzedRoute{
		Method:         httpMethods[method],
		Path:           parentPrefix + routePath,
		ControllerType: ctrlType,
		MethodName:     methodName,
		Middleware:      allMW,
		File:           file,
		Line:           fset.Position(call.Pos()).Line,
	}, true
}

// extractStringLit extracts a string value from a *ast.BasicLit.
func extractStringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	return strings.Trim(lit.Value, `"`)
}

// extractMiddlewareName extracts the middleware function name from an expression.
// Handles: middleware.Auth → "Auth", middleware.RequireRole("admin") → "RequireRole"
func extractMiddlewareName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.CallExpr:
		// Parameterized middleware like middleware.RequireRole("admin")
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			return sel.Sel.Name
		}
	}
	return ""
}

// extractHandlerRef extracts controller type and method name from a handler expression.
// Handles: controllers.PostController{}.Store → ("PostController", "Store")
func extractHandlerRef(expr ast.Expr) (ctrlType, method string) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}

	method = sel.Sel.Name

	// The X should be a composite literal: controllers.PostController{}
	switch x := sel.X.(type) {
	case *ast.CompositeLit:
		ctrlType = extractControllerType(x)
	case *ast.Ident:
		ctrlType = x.Name
	}

	return ctrlType, method
}

// extractControllerType extracts the type name from a composite literal or expression.
func extractControllerType(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		switch t := e.Type.(type) {
		case *ast.SelectorExpr:
			return t.Sel.Name
		case *ast.Ident:
			return t.Name
		}
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.Ident:
		return e.Name
	}
	return ""
}
