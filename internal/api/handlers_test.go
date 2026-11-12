package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubedeck/kubedeck/internal/kube"
)

type fakeTransport struct {
	name       string
	namespaces []string
	list       *unstructured.UnstructuredList
	object     *unstructured.Unstructured
	err        error
}

func (f *fakeTransport) Name() string { return f.name }
func (f *fakeTransport) Info(context.Context) kube.ClusterInfo {
	return kube.ClusterInfo{Name: f.name, Version: "v1.37.2", Status: kube.StatusHealthy}
}
func (f *fakeTransport) Namespaces(context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.namespaces, nil
}
func (f *fakeTransport) List(context.Context, schema.GroupVersionResource, string, kube.ListOptions) (*unstructured.UnstructuredList, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}
func (f *fakeTransport) Get(context.Context, schema.GroupVersionResource, string, string) (*unstructured.Unstructured, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.object, nil
}

type fakeRegistry struct {
	items map[string]kube.Transport
	order []string
}

func (f *fakeRegistry) Get(name string) (kube.Transport, bool) {
	t, ok := f.items[name]
	return t, ok
}

func (f *fakeRegistry) All() []kube.Transport {
	out := make([]kube.Transport, 0, len(f.order))
	for _, name := range f.order {
		if t, ok := f.items[name]; ok {
			out = append(out, t)
		}
	}
	return out
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func testDeployment() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":          "web",
			"namespace":     "default",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
		},
		"spec": map[string]any{"replicas": int64(2)},
	}}
}

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	transport := &fakeTransport{
		name:       "prod",
		namespaces: []string{"default", "kube-system"},
		list: &unstructured.UnstructuredList{
			Items: []unstructured.Unstructured{*testDeployment()},
		},
		object: testDeployment(),
	}
	registry := &fakeRegistry{
		items: map[string]kube.Transport{"prod": transport},
		order: []string{"prod"},
	}
	return NewRouter(Options{Registry: registry})
}

func doRequest(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListClusters(t *testing.T) {
	rec := doRequest(t, testRouter(t), http.MethodGet, "/api/v1/clusters")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var clusters []kube.ClusterInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &clusters); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(clusters) != 1 || clusters[0].Name != "prod" || clusters[0].Version != "v1.37.2" {
		t.Fatalf("clusters = %+v", clusters)
	}
}

func TestUnknownClusterReturns404(t *testing.T) {
	rec := doRequest(t, testRouter(t), http.MethodGet, "/api/v1/clusters/missing/namespaces")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeNotFound || body.RequestID == "" {
		t.Fatalf("error = %+v, want NOT_FOUND with requestId", body)
	}
}

func TestListNamespaces(t *testing.T) {
	rec := doRequest(t, testRouter(t), http.MethodGet, "/api/v1/clusters/prod/namespaces")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Namespaces []string `json:"namespaces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Namespaces) != 2 || body.Namespaces[0] != "default" {
		t.Fatalf("namespaces = %v", body.Namespaces)
	}
}

func TestListResources(t *testing.T) {
	rec := doRequest(t, testRouter(t), http.MethodGet,
		"/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0]["kind"] != "Deployment" {
		t.Fatalf("items = %+v", body.Items)
	}
}

func TestListResourcesRejectsInvalidLimit(t *testing.T) {
	rec := doRequest(t, testRouter(t), http.MethodGet,
		"/api/v1/clusters/prod/resources/apps/v1/deployments?limit=0")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeInvalidArgument {
		t.Fatalf("code = %s, want INVALID_ARGUMENT", body.Code)
	}
}

func TestGetResourceAsYAML(t *testing.T) {
	rec := doRequest(t, testRouter(t), http.MethodGet,
		"/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default&format=yaml")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/yaml") {
		t.Fatalf("Content-Type = %q, want text/yaml", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "kind: Deployment") || !strings.Contains(body, "name: web") {
		t.Fatalf("yaml body = %q", body)
	}
	if strings.Contains(body, "managedFields") {
		t.Fatal("managedFields should be stripped from YAML output")
	}
}

func TestHealthAndReadiness(t *testing.T) {
	healthy := NewRouter(Options{})
	rec := doRequest(t, healthy, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	rec = doRequest(t, healthy, http.MethodGet, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz without dependencies status = %d, want 200", rec.Code)
	}

	unready := NewRouter(Options{Pinger: fakePinger{err: context.DeadlineExceeded}})
	rec = doRequest(t, unready, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz must stay 200 when dependency fails, got %d", rec.Code)
	}
	rec = doRequest(t, unready, http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", rec.Code)
	}
}
