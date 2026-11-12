package api

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// TestOpenAPIMatchesRoutes 保证 docs/openapi.yaml 与路由注册一致。
func TestOpenAPIMatchesRoutes(t *testing.T) {
	router, ok := NewRouter(Options{}).(*chi.Mux)
	if !ok {
		t.Fatal("router is not *chi.Mux")
	}

	got := make(map[string]bool)
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// SPA 静态资源 fallback 不属于 API 契约。
		if route == "/*" {
			return nil
		}
		got[method+" "+normalizeRoute(route)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	data, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}

	want := make(map[string]bool)
	for path, operations := range spec.Paths {
		for method := range operations {
			switch strings.ToUpper(method) {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
				http.MethodDelete, http.MethodHead, http.MethodOptions:
				want[strings.ToUpper(method)+" "+normalizeRoute(path)] = true
			}
		}
	}

	for key := range got {
		if !want[key] {
			t.Errorf("route registered but missing from openapi: %s", key)
		}
	}
	for key := range want {
		if !got[key] {
			t.Errorf("openapi documents route not registered: %s", key)
		}
	}
	if t.Failed() {
		t.Logf("registered routes:\n%s", strings.Join(sortedKeys(got), "\n"))
	}
}

func normalizeRoute(route string) string {
	if route == "/" {
		return route
	}
	return strings.TrimSuffix(route, "/")
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
