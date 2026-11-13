package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

func writeTestRouter(t *testing.T, cluster *fakeCluster) (http.Handler, *fakeTransport) {
	t.Helper()
	transport := &fakeTransport{name: "prod", namespaces: []string{"default"}, object: testDeployment()}
	if cluster == nil {
		cluster = &fakeCluster{t: transport}
	} else {
		cluster.t = transport
	}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": cluster},
		order: []string{"prod"},
	}
	return NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}},
	}), transport
}

func doRequestWithBody(t *testing.T, h http.Handler, method, target, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) Error {
	t.Helper()
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v (body=%s)", err, rec.Body.String())
	}
	return body
}

const deploymentYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web2
spec:
  replicas: 2
`

func TestCreateResource(t *testing.T) {
	router, transport := writeTestRouter(t, nil)
	rec := doRequestWithBody(t, router, http.MethodPost,
		"/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
		"application/yaml", deploymentYAML)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if transport.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", transport.createCalls)
	}
	if transport.createdObject.GetName() != "web2" {
		t.Fatalf("created object name = %q", transport.createdObject.GetName())
	}
	if len(transport.createDryRun) != 0 {
		t.Fatalf("dryRun = %v, want empty", transport.createDryRun)
	}
}

func TestCreateResourceDryRunYAML(t *testing.T) {
	router, transport := writeTestRouter(t, nil)
	rec := doRequestWithBody(t, router, http.MethodPost,
		"/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default&dryRun=All&format=yaml",
		"application/yaml", deploymentYAML)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/yaml") {
		t.Fatalf("content-type = %q, want text/yaml", ct)
	}
	if len(transport.createDryRun) != 1 || transport.createDryRun[0] != "All" {
		t.Fatalf("dryRun = %v, want [All]", transport.createDryRun)
	}
}

func TestCreateResourceAlreadyExists(t *testing.T) {
	router, transport := writeTestRouter(t, nil)
	transport.err = apierrors.NewAlreadyExists(schema.GroupResource{Group: "apps", Resource: "deployments"}, "web2")

	rec := doRequestWithBody(t, router, http.MethodPost,
		"/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
		"application/yaml", deploymentYAML)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if body := decodeError(t, rec); body.Code != CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", body.Code)
	}
}

func TestCreateResourceValidation(t *testing.T) {
	nodeCluster := &fakeCluster{resources: []kube.APIResource{
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true,
			Verbs: []string{"get", "list", "create"}},
		{Version: "v1", Resource: "nodes", Kind: "Node", Namespaced: false,
			Verbs: []string{"get", "list", "create"}},
	}}
	readOnlyCluster := &fakeCluster{resources: []kube.APIResource{
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true,
			Verbs: []string{"get", "list"}},
	}}

	cases := []struct {
		name       string
		target     string
		body       string
		cluster    *fakeCluster
		wantStatus int
		wantCode   ErrorCode
	}{
		{
			name:       "namespaced resource without namespace",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments",
			body:       deploymentYAML,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeInvalidArgument,
		},
		{
			name:       "cluster scoped resource with namespace",
			target:     "/api/v1/clusters/prod/resources/_/v1/nodes?namespace=default",
			body:       "apiVersion: v1\nkind: Node\nmetadata:\n  name: n1\n",
			cluster:    nodeCluster,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeInvalidArgument,
		},
		{
			name:       "kind mismatch",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
			body:       strings.Replace(deploymentYAML, "kind: Deployment", "kind: StatefulSet", 1),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   CodeUnprocessable,
		},
		{
			name:       "missing name",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
			body:       strings.Replace(deploymentYAML, "  name: web2\n", "", 1),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   CodeUnprocessable,
		},
		{
			name:       "namespace mismatch",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
			body:       strings.Replace(deploymentYAML, "  name: web2", "  name: web2\n  namespace: other", 1),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   CodeUnprocessable,
		},
		{
			name:       "resource version present",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
			body:       strings.Replace(deploymentYAML, "  name: web2", "  name: web2\n  resourceVersion: \"1\"", 1),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   CodeUnprocessable,
		},
		{
			name:       "create verb unsupported",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
			body:       deploymentYAML,
			cluster:    readOnlyCluster,
			wantStatus: http.StatusBadRequest,
			wantCode:   CodeUnsupported,
		},
		{
			name:       "api version mismatch",
			target:     "/api/v1/clusters/prod/resources/apps/v1/deployments?namespace=default",
			body:       strings.Replace(deploymentYAML, "apiVersion: apps/v1", "apiVersion: apps/v1beta1", 1),
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   CodeUnprocessable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, transport := writeTestRouter(t, tc.cluster)
			rec := doRequestWithBody(t, router, http.MethodPost, tc.target, "application/yaml", tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := decodeError(t, rec).Code; got != tc.wantCode {
				t.Fatalf("code = %s, want %s", got, tc.wantCode)
			}
			if transport.createCalls != 0 {
				t.Fatalf("create calls = %d, want 0 before upstream", transport.createCalls)
			}
		})
	}
}

func TestCreateResourcePayloadTooLarge(t *testing.T) {
	router, _ := writeTestRouter(t, nil)
	body := "apiVersion: v1\nkind: Pod\nmetadata:\n  name: p1\n  annotations:\n    big: " +
		strings.Repeat("a", maxYAMLBytes+16)
	rec := doRequestWithBody(t, router, http.MethodPost,
		"/api/v1/clusters/prod/resources/_/v1/pods?namespace=default", "application/yaml", body)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestPatchResourceMerge(t *testing.T) {
	router, transport := writeTestRouter(t, nil)
	rec := doRequestWithBody(t, router, http.MethodPatch,
		"/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
		"application/merge-patch+json", `{"spec":{"replicas":3}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if transport.patchCalls != 1 {
		t.Fatalf("patch calls = %d, want 1", transport.patchCalls)
	}
	if transport.patchType != types.MergePatchType {
		t.Fatalf("patch type = %s, want merge", transport.patchType)
	}
	if !strings.Contains(string(transport.patchData), `"replicas":3`) {
		t.Fatalf("patch data = %s", transport.patchData)
	}
	if transport.patchOpts.FieldManager != "kubedeck" || transport.patchOpts.Force {
		t.Fatalf("patch opts = %+v", transport.patchOpts)
	}
}

func TestPatchResourceStrategicAndApply(t *testing.T) {
	t.Run("strategic merge", func(t *testing.T) {
		router, transport := writeTestRouter(t, nil)
		rec := doRequestWithBody(t, router, http.MethodPatch,
			"/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
			"application/strategic-merge-patch+json", `{"spec":{"replicas":4}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if transport.patchType != types.StrategicMergePatchType {
			t.Fatalf("patch type = %s, want strategic merge", transport.patchType)
		}
	})

	t.Run("apply with force", func(t *testing.T) {
		router, transport := writeTestRouter(t, nil)
		body := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
`
		rec := doRequestWithBody(t, router, http.MethodPatch,
			"/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default&fieldManager=kd&force=true&dryRun=All",
			"application/apply-patch+yaml", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if transport.patchType != types.ApplyPatchType {
			t.Fatalf("patch type = %s, want apply", transport.patchType)
		}
		if transport.patchOpts.FieldManager != "kd" || !transport.patchOpts.Force {
			t.Fatalf("patch opts = %+v", transport.patchOpts)
		}
		if len(transport.patchOpts.DryRun) != 1 || transport.patchOpts.DryRun[0] != "All" {
			t.Fatalf("dryRun = %v, want [All]", transport.patchOpts.DryRun)
		}
	})
}

func TestPatchResourceValidation(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		contentType string
		body        string
		wantStatus  int
		wantCode    ErrorCode
	}{
		{
			name:        "apply without field manager",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
			contentType: "application/apply-patch+yaml",
			body:        deploymentYAML,
			wantStatus:  http.StatusBadRequest,
			wantCode:    CodeInvalidArgument,
		},
		{
			name:        "force on merge patch",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default&force=true",
			contentType: "application/merge-patch+json",
			body:        `{"spec":{"replicas":3}}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    CodeInvalidArgument,
		},
		{
			name:        "unsupported media type",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
			contentType: "application/json-patch+json",
			body:        `[]`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    CodeUnsupported,
		},
		{
			name:        "apply without name",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default&fieldManager=kd",
			contentType: "application/apply-patch+yaml",
			body:        "apiVersion: apps/v1\nkind: Deployment\n",
			wantStatus:  http.StatusUnprocessableEntity,
			wantCode:    CodeUnprocessable,
		},
		{
			name:        "name mismatch",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
			contentType: "application/merge-patch+json",
			body:        `{"metadata":{"name":"other"}}`,
			wantStatus:  http.StatusUnprocessableEntity,
			wantCode:    CodeUnprocessable,
		},
		{
			name:        "api version mismatch",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
			contentType: "application/merge-patch+json",
			body:        `{"apiVersion":"apps/v1beta1"}`,
			wantStatus:  http.StatusUnprocessableEntity,
			wantCode:    CodeUnprocessable,
		},
		{
			name:        "empty body",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default",
			contentType: "application/merge-patch+json",
			body:        "   ",
			wantStatus:  http.StatusBadRequest,
			wantCode:    CodeInvalidArgument,
		},
		{
			name:        "invalid force value",
			target:      "/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default&force=yes",
			contentType: "application/merge-patch+json",
			body:        `{"spec":{"replicas":3}}`,
			wantStatus:  http.StatusBadRequest,
			wantCode:    CodeInvalidArgument,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, transport := writeTestRouter(t, nil)
			rec := doRequestWithBody(t, router, http.MethodPatch, tc.target, tc.contentType, tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := decodeError(t, rec).Code; got != tc.wantCode {
				t.Fatalf("code = %s, want %s", got, tc.wantCode)
			}
			if transport.patchCalls != 0 {
				t.Fatalf("patch calls = %d, want 0 ", transport.patchCalls)
			}
		})
	}
}

func TestPatchResourceConflicts(t *testing.T) {
	router, transport := writeTestRouter(t, nil)
	transport.err = apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "web",
		errors.New("field conflict"))

	rec := doRequestWithBody(t, router, http.MethodPatch,
		"/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default&fieldManager=kd",
		"application/apply-patch+yaml", strings.Replace(deploymentYAML, "name: web2", "name: web", 1))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if body := decodeError(t, rec); body.Code != CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", body.Code)
	}
}

func TestPatchAuditVerb(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch,
		"/api/v1/clusters/prod/resources/apps/v1/deployments/web?namespace=default", nil)
	verb, group, resource, namespace, name := auditTarget(req)
	if verb != "patch" || group != "apps" || resource != "deployments" || namespace != "default" || name != "web" {
		t.Fatalf("audit = %s %s %s %s %s", verb, group, resource, namespace, name)
	}
}
