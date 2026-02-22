package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ParsedRoute represents a single route extracted from a routes.go file.
type ParsedRoute struct {
	Method     string   // HTTP method: GET, POST, PUT, PATCH, DELETE
	Path       string   // Full resolved path (e.g. /api/users/:id)
	Controller string   // Controller type name (e.g. UserController)
	Action     string   // Method name (e.g. Index, Store)
	Middleware []string // Middleware references as source strings
}

// ParseRoutes parses a routes.go file and extracts all route definitions.
func ParseRoutes(filename string, src []byte) ([]ParsedRoute, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}

	var routes []ParsedRoute

	// Walk the AST looking for calls to Routes() or pickle.Routes()
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		if !isRoutesCall(call) {
			return true
		}

		if len(call.Args) != 1 {
			return true
		}
		funcLit, ok := call.Args[0].(*ast.FuncLit)
		if !ok {
			return true
		}

		// Get the router parameter name (usually "r")
		routerVar := "r"
		if len(funcLit.Type.Params.List) > 0 && len(funcLit.Type.Params.List[0].Names) > 0 {
			routerVar = funcLit.Type.Params.List[0].Names[0].Name
		}

		routes = append(routes, extractRoutes(funcLit.Body, routerVar, "", nil)...)
		return false
	})

	return routes, nil
}

func isRoutesCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "Routes"
	case *ast.SelectorExpr:
		// pickle.Routes
		if ident, ok := fn.X.(*ast.Ident); ok {
			return ident.Name == "pickle" && fn.Sel.Name == "Routes"
		}
	}
	return false
}

func extractRoutes(body *ast.BlockStmt, routerVar, prefix string, groupMW []string) []ParsedRoute {
	var routes []ParsedRoute

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
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != routerVar {
			continue
		}

		methodName := sel.Sel.Name
		switch methodName {
		case "Get", "Post", "Put", "Patch", "Delete":
			httpMethod := strings.ToUpper(methodName)
			if methodName == "Patch" {
				httpMethod = "PATCH"
			}
			route := parseRouteCall(call, httpMethod, prefix, groupMW)
			if route != nil {
				routes = append(routes, *route)
			}
		case "Group":
			routes = append(routes, parseGroupCall(call, prefix, groupMW)...)
		case "Resource":
			routes = append(routes, parseResourceCall(call, prefix, groupMW)...)
		}
	}

	return routes
}

// parseRouteCall handles r.Get("/path", Controller{}.Method, mw...)
func parseRouteCall(call *ast.CallExpr, method, prefix string, groupMW []string) *ParsedRoute {
	if len(call.Args) < 2 {
		return nil
	}

	path := stringLitValue(call.Args[0])
	if path == "" {
		return nil
	}

	controller, action := parseHandler(call.Args[1])
	if controller == "" || action == "" {
		return nil
	}

	// Additional args after the handler are per-route middleware
	var mw []string
	mw = append(mw, groupMW...)
	for _, arg := range call.Args[2:] {
		mw = append(mw, exprToString(arg))
	}

	return &ParsedRoute{
		Method:     method,
		Path:       prefix + path,
		Controller: controller,
		Action:     action,
		Middleware: mw,
	}
}

// parseGroupCall handles r.Group("/prefix", mw..., func(r *Router) { ... })
func parseGroupCall(call *ast.CallExpr, parentPrefix string, parentMW []string) []ParsedRoute {
	if len(call.Args) < 2 {
		return nil
	}

	prefix := stringLitValue(call.Args[0])

	var middleware []string
	middleware = append(middleware, parentMW...)

	var funcLit *ast.FuncLit

	// Everything between the prefix and the func literal is middleware
	for _, arg := range call.Args[1:] {
		if fl, ok := arg.(*ast.FuncLit); ok {
			funcLit = fl
		} else {
			middleware = append(middleware, exprToString(arg))
		}
	}

	if funcLit == nil {
		return nil
	}

	// Get the router parameter name from the nested function
	routerVar := "r"
	if len(funcLit.Type.Params.List) > 0 && len(funcLit.Type.Params.List[0].Names) > 0 {
		routerVar = funcLit.Type.Params.List[0].Names[0].Name
	}

	return extractRoutes(funcLit.Body, routerVar, parentPrefix+prefix, middleware)
}

// parseResourceCall handles r.Resource("/users", UserController{}, mw...)
// Expands to standard CRUD routes.
func parseResourceCall(call *ast.CallExpr, prefix string, groupMW []string) []ParsedRoute {
	if len(call.Args) < 2 {
		return nil
	}

	resourcePrefix := stringLitValue(call.Args[0])
	controller := compositeTypeName(call.Args[1])
	if controller == "" {
		return nil
	}

	// Additional args are middleware
	var mw []string
	mw = append(mw, groupMW...)
	for _, arg := range call.Args[2:] {
		mw = append(mw, exprToString(arg))
	}

	fullPrefix := prefix + resourcePrefix

	return []ParsedRoute{
		{Method: "GET", Path: fullPrefix, Controller: controller, Action: "Index", Middleware: mw},
		{Method: "GET", Path: fullPrefix + "/:id", Controller: controller, Action: "Show", Middleware: mw},
		{Method: "POST", Path: fullPrefix, Controller: controller, Action: "Store", Middleware: mw},
		{Method: "PUT", Path: fullPrefix + "/:id", Controller: controller, Action: "Update", Middleware: mw},
		{Method: "DELETE", Path: fullPrefix + "/:id", Controller: controller, Action: "Destroy", Middleware: mw},
	}
}

// parseHandler extracts controller name and method from Controller{}.Method
func parseHandler(expr ast.Expr) (controller, method string) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}

	method = sel.Sel.Name

	// Controller{}.Method — X is a CompositeLit
	if comp, ok := sel.X.(*ast.CompositeLit); ok {
		controller = compositeTypeName(comp)
		return controller, method
	}

	// Could also be a plain identifier: ctrl.Method
	if ident, ok := sel.X.(*ast.Ident); ok {
		return ident.Name, method
	}

	return "", ""
}

// compositeTypeName extracts the type name from a composite literal like UserController{}
func compositeTypeName(expr ast.Expr) string {
	comp, ok := expr.(*ast.CompositeLit)
	if !ok {
		return ""
	}
	switch t := comp.Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	}
	return ""
}

// stringLitValue extracts the string value from a basic literal.
func stringLitValue(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	return strings.Trim(lit.Value, `"`)
}

// exprToString renders an AST expression back to a source string.
// Used for middleware references.
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.CallExpr:
		fn := exprToString(e.Fun)
		var args []string
		for _, a := range e.Args {
			args = append(args, exprToString(a))
		}
		return fn + "(" + strings.Join(args, ", ") + ")"
	case *ast.BasicLit:
		return e.Value
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	default:
		return "/* unsupported */"
	}
}
