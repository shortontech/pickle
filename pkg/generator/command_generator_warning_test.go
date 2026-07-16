package generator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNonControllerHandlerWarningsInspectOnlyRouteHandlers(t *testing.T) {
	routesDir := t.TempDir()
	source := `package routes

import (
	"log/slog"
	"example.com/app/app/http/controllers"
	"example.com/app/observability"
	pickle "example.com/app/app/http"
)

var API = pickle.Routes(func(r *pickle.Router) {
	observability.Set(&observability.LoggingMetrics{Logger: slog.Default()})
	r.Get("/messages", controllers.MessageController{}.Index)
})
`
	if err := os.WriteFile(filepath.Join(routesDir, "web.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	if warnings := findNonControllerHandlers(routesDir); len(warnings) != 0 {
		t.Fatalf("non-handler composite literal produced warnings: %+v", warnings)
	}
}

func TestNonControllerHandlerWarningsStillReportActualHandlers(t *testing.T) {
	routesDir := t.TempDir()
	source := `package routes

var API = pickle.Routes(func(r *pickle.Router) {
	r.Get("/health", services.HealthHandler{}.Show)
	r.Resource("/legacy", legacy.ResourceController{})
})
`
	if err := os.WriteFile(filepath.Join(routesDir, "web.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	warnings := findNonControllerHandlers(routesDir)
	if len(warnings) != 2 {
		t.Fatalf("warnings = %+v, want two actual handler warnings", warnings)
	}
	if warnings[0].packageName != "services" || warnings[1].packageName != "legacy" {
		t.Fatalf("warning packages = %q, %q", warnings[0].packageName, warnings[1].packageName)
	}
}
