package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

type fakeTransport struct {
	name        string
	namespaces  []string
	list        *unstructured.UnstructuredList
	object      *unstructured.Unstructured
	err         error
	deleteErrs  map[string]error
	updateCalls int
	logBody     string
	watcher     watch.Interface
	watchErr    error
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
func (f *fakeTransport) ReviewAccess(context.Context, kube.AccessRequest) (kube.AccessResult, error) {
	return kube.AccessResult{Allowed: true}, nil
}
func (f *fakeTransport) Create(context.Context, schema.GroupVersionResource, string, *unstructured.Unstructured, []string) (*unstructured.Unstructured, error) {
	return f.object, f.err
}
func (f *fakeTransport) Update(context.Context, schema.GroupVersionResource, string, *unstructured.Unstructured, []string) (*unstructured.Unstructured, error) {
	f.updateCalls++
	return f.object, f.err
}
func (f *fakeTransport) Delete(_ context.Context, _ schema.GroupVersionResource, _ string, name string, _ kube.DeleteOptions) error {
	if f.deleteErrs != nil {
		if err, ok := f.deleteErrs[name]; ok {
			return err
		}
	}
	return f.err
}

func (f *fakeTransport) Scale(context.Context, schema.GroupVersionResource, string, string, int32) (*unstructured.Unstructured, error) {
	return f.object, f.err
}
func (f *fakeTransport) Evict(context.Context, string, string, *int64) error {
	return f.err
}

func (f *fakeTransport) Logs(context.Context, string, string, kube.LogOptions) (io.ReadCloser, error) {
	body := f.logBody
	if body == "" {
		body = "log line\n"
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func (f *fakeTransport) Watch(context.Context, schema.GroupVersionResource, string, kube.WatchOptions) (watch.Interface, error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	if f.watcher != nil {
		return f.watcher, nil
	}
	return watch.NewFake(), nil
}

type fakeCluster struct{ t *fakeTransport }

func (c *fakeCluster) Name() string { return c.t.name }
func (c *fakeCluster) Info(context.Context) kube.ClusterInfo {
	return kube.ClusterInfo{Name: c.t.name, Version: "v1.37.2", Status: kube.StatusHealthy}
}
func (c *fakeCluster) TransportFor(kube.UserRef) (kube.Transport, error) { return c.t, nil }
func (c *fakeCluster) Discovery(context.Context) (kube.DiscoveryResult, error) {
	return kube.DiscoveryResult{Resources: []kube.APIResource{
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true},
		{Version: "v1", Resource: "secrets", Kind: "Secret", Namespaced: true},
		{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true},
	}}, nil
}

type fakeRegistry struct {
	items map[string]kube.ClusterAccess
	order []string
}

func (f *fakeRegistry) Get(name string) (kube.ClusterAccess, bool) {
	c, ok := f.items[name]
	return c, ok
}

func (f *fakeRegistry) All() []kube.ClusterAccess {
	out := make([]kube.ClusterAccess, 0, len(f.order))
	for _, name := range f.order {
		if c, ok := f.items[name]; ok {
			out = append(out, c)
		}
	}
	return out
}

type fakeAuth struct {
	identity      auth.Identity
	authenticated bool
	dev           bool
}

func (f fakeAuth) DevMode() bool { return f.dev }
func (f fakeAuth) DevIdentity() (auth.Identity, bool) {
	return f.identity, f.dev
}
func (f fakeAuth) NewDevSession() (auth.Session, error) {
	return auth.Session{ID: "test", Identity: f.identity, ExpiresAt: 1 << 40}, nil
}
func (f fakeAuth) StartLogin(string) (string, string, error) { return "/auth/mock", "state", nil }
func (f fakeAuth) FinishLogin(context.Context, string, string, string) (auth.Session, string, error) {
	return auth.Session{}, "/", nil
}
func (f fakeAuth) Session(string) (auth.Session, bool) {
	if !f.authenticated {
		return auth.Session{}, false
	}
	return auth.Session{ID: "test", Identity: f.identity}, true
}
func (f fakeAuth) Logout(string)    {}
func (f fakeAuth) LogoutUser(int64) {}

type fakeAuthStore struct {
	bindings []domain.RoleBinding
}

func (f fakeAuthStore) BindingsForUser(context.Context, int64) ([]domain.RoleBinding, error) {
	return f.bindings, nil
}
func (f fakeAuthStore) ClustersForUser(context.Context, int64) ([]domain.Cluster, error) {
	return nil, nil
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
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	return NewRouter(Options{Registry: registry, Auth: fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}}})
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
	healthy := NewRouter(Options{Auth: fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}}})
	rec := doRequest(t, healthy, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	rec = doRequest(t, healthy, http.MethodGet, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz without dependencies status = %d, want 200", rec.Code)
	}

	unready := NewRouter(Options{Auth: fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}}, Pinger: fakePinger{err: context.DeadlineExceeded}})
	rec = doRequest(t, unready, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz must stay 200 when dependency fails, got %d", rec.Code)
	}
	rec = doRequest(t, unready, http.MethodGet, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", rec.Code)
	}
}

func TestUnauthenticatedReturns401(t *testing.T) {
	rec := doRequest(t, NewRouter(Options{Auth: fakeAuth{}}), http.MethodGet, "/api/v1/clusters")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != CodeUnauthenticated {
		t.Fatalf("code = %s, want UNAUTHENTICATED", body.Code)
	}
}

func TestUnauthorizedClusterReturns403WithoutName(t *testing.T) {
	transport := &fakeTransport{name: "prod", namespaces: []string{"default"}}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry:  registry,
		Auth:      fakeAuth{authenticated: true, identity: auth.Identity{Subject: "viewer"}},
		AuthStore: fakeAuthStore{},
	})
	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/prod/namespaces")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "prod") {
		t.Fatalf("403 body must not leak cluster name: %s", rec.Body.String())
	}
}

func TestCanBatchReviewWithBinding(t *testing.T) {
	transport := &fakeTransport{name: "prod", namespaces: []string{"default"}}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	store := fakeAuthStore{bindings: []domain.RoleBinding{{
		UserID: 9, ClusterName: "prod", Namespace: "default", RoleCode: "viewer",
	}}}
	router := NewRouter(Options{
		Registry:  registry,
		Auth:      fakeAuth{authenticated: true, identity: auth.Identity{UserID: 9, Subject: "viewer"}},
		AuthStore: store,
	})
	body := `{"checks":[{"cluster":"prod","namespace":"default","verb":"delete","resource":"pods"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/can", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []accessResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || !resp.Results[0].Allowed {
		t.Fatalf("results = %+v", resp.Results)
	}
}

func TestBatchDeleteReportsPartialFailure(t *testing.T) {
	transport := &fakeTransport{
		name: "prod",
		deleteErrs: map[string]error{
			"missing": apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "missing"),
		},
	}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}},
	})
	body := `{"names":["web","missing"],"propagationPolicy":"Foreground","dryRun":true}`
	req := httptest.NewRequest(http.MethodDelete,
		"/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results   []deleteItemResult `json:"results"`
		Succeeded int                `json:"succeeded"`
		Failed    int                `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Succeeded != 1 || resp.Failed != 1 || len(resp.Results) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Results[1].Success || resp.Results[1].Code != string(CodeNotFound) {
		t.Fatalf("missing result = %+v", resp.Results[1])
	}
}

func workloadRouter(transport *fakeTransport) http.Handler {
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	return NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{UserID: 1, Subject: "tester", PlatformAdmin: true}},
	})
}

func deploymentObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name": "web", "namespace": "default", "uid": "uid-1", "generation": int64(2),
			"annotations": map[string]any{"deployment.kubernetes.io/revision": "3"},
		},
		"spec": map[string]any{
			"replicas": int64(2),
			"template": map[string]any{"spec": map[string]any{"containers": []any{
				map[string]any{"name": "web", "image": "nginx:1.25"},
			}}},
		},
		"status": map[string]any{
			"replicas": int64(2), "readyReplicas": int64(2), "updatedReplicas": int64(2),
			"observedGeneration": int64(2),
		},
	}}
}

func TestScaleCreatesOperation(t *testing.T) {
	transport := &fakeTransport{name: "prod", object: deploymentObject()}
	router := workloadRouter(transport)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/clusters/prod/workloads/apps/v1/deployments/web/scale?namespace=default",
		strings.NewReader(`{"replicas":5}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OperationID    string `json:"operationId"`
		TargetReplicas int64  `json:"targetReplicas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OperationID == "" || resp.TargetReplicas != 5 {
		t.Fatalf("resp = %+v", resp)
	}

	// 查询操作进度。
	req = httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/operations/"+resp.OperationID, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operation status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRestartIsIdempotent(t *testing.T) {
	transport := &fakeTransport{name: "prod", object: deploymentObject()}
	router := workloadRouter(transport)
	doRestart := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/clusters/prod/workloads/apps/v1/deployments/web/restart?namespace=default", nil)
		req.Header.Set("Idempotency-Key", "restart-1")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	first := doRestart()
	second := doRestart()
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d/%d body=%s", first.Code, second.Code, second.Body.String())
	}
	if transport.updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1 (idempotent retry)", transport.updateCalls)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("retry response differs: %s vs %s", first.Body.String(), second.Body.String())
	}
}

func TestScaleRejectsUnsupportedResource(t *testing.T) {
	transport := &fakeTransport{name: "prod", object: deploymentObject()}
	router := workloadRouter(transport)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/clusters/prod/workloads/_/v1/pods/web/scale?namespace=default",
		strings.NewReader(`{"replicas":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRollbackUnavailableRevision(t *testing.T) {
	transport := &fakeTransport{name: "prod", object: deploymentObject(), list: &unstructured.UnstructuredList{}}
	router := workloadRouter(transport)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/clusters/prod/workloads/apps/v1/deployments/web/rollback?namespace=default",
		strings.NewReader(`{"revision":99}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
