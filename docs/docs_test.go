package docs_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kestrix/docs"
	"kestrix/internal/web"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

func TestSwaggerInfo(t *testing.T) {
	if docs.SwaggerInfo == nil {
		t.Fatal("SwaggerInfo is nil")
	}
	if docs.SwaggerInfo.InstanceName() != "swagger" {
		t.Fatalf("InstanceName() = %q, want %q", docs.SwaggerInfo.InstanceName(), "swagger")
	}
	if docs.SwaggerInfo.Title != "Kestrix API" {
		t.Fatalf("Title = %q, want %q", docs.SwaggerInfo.Title, "Kestrix API")
	}
}

func findUndocumentedRoutes(router chi.Routes, paths map[string]map[string]interface{}) ([]string, error) {
	var undocumented []string
	err := chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		// Only check /api/* and /webhook routes; non-API endpoints like /, /ping, /docs/*, /metrics are excluded.
		if !strings.HasPrefix(route, "/api/") && route != "/webhook" {
			return nil
		}

		pathItem, ok := paths[route]
		if !ok {
			undocumented = append(undocumented, fmt.Sprintf("%s %s (missing path)", method, route))
			return nil
		}

		lowerMethod := strings.ToLower(method)
		if _, ok := pathItem[lowerMethod]; !ok {
			undocumented = append(undocumented, fmt.Sprintf("%s %s (missing method)", method, route))
		}
		return nil
	})
	return undocumented, err
}

func loadSwaggerPaths(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	yamlPath := "swagger.yaml"
	if _, err := os.Stat(yamlPath); err != nil {
		yamlPath = filepath.Join("docs", "swagger.yaml")
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", yamlPath, err)
	}

	var spec struct {
		Paths map[string]map[string]interface{} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("failed to parse %s: %v", yamlPath, err)
	}
	return spec.Paths
}

func TestRouteParity(t *testing.T) {
	srv := web.NewServerWithDeps(web.Config{}, nil, nil, nil, nil, nil, nil, nil)
	router, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("srv.Router() does not implement chi.Routes")
	}

	paths := loadSwaggerPaths(t)
	undocumented, err := findUndocumentedRoutes(router, paths)
	if err != nil {
		t.Fatalf("findUndocumentedRoutes failed: %v", err)
	}

	if len(undocumented) > 0 {
		t.Errorf("found %d registered route(s) missing from swagger.yaml:\n  %s",
			len(undocumented), strings.Join(undocumented, "\n  "))
	}
}

func TestRouteParity_DetectsUndocumentedRoute(t *testing.T) {
	dummyRouter := chi.NewRouter()
	dummyRouter.Get("/api/sample/undocumented", func(w http.ResponseWriter, r *http.Request) {})
	dummyRouter.Post("/webhook", func(w http.ResponseWriter, r *http.Request) {})

	paths := loadSwaggerPaths(t)
	undocumented, err := findUndocumentedRoutes(dummyRouter, paths)
	if err != nil {
		t.Fatalf("findUndocumentedRoutes failed: %v", err)
	}

	if len(undocumented) != 1 {
		t.Fatalf("expected exactly 1 undocumented route, got %d: %v", len(undocumented), undocumented)
	}
	if !strings.Contains(undocumented[0], "GET /api/sample/undocumented") {
		t.Fatalf("expected undocumented route to mention GET /api/sample/undocumented, got %s", undocumented[0])
	}
}
