package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

func overviewRouter(transport *fakeTransport, identity auth.Identity, bindings []domain.RoleBinding) http.Handler {
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	return NewRouter(Options{
		Registry:  registry,
		Auth:      fakeAuth{authenticated: true, identity: identity},
		AuthStore: fakeAuthStore{bindings: bindings},
	})
}

func overviewPod(name string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"status":     map[string]any{"phase": "Running"},
	}
}

func TestClusterOverview(t *testing.T) {
	transport := &fakeTransport{
		name:       "prod",
		namespaces: []string{"default"},
		lists: map[string]*unstructured.UnstructuredList{
			"pods": {Items: []unstructured.Unstructured{{Object: overviewPod("p1")}}},
			"nodes": {Items: []unstructured.Unstructured{{Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Node",
				"metadata":   map[string]any{"name": "n1"},
				"status": map[string]any{
					"conditions":  []any{map[string]any{"type": "Ready", "status": "True"}},
					"allocatable": map[string]any{"cpu": "2", "memory": "4Gi"},
				},
			}}}},
		},
	}
	router := overviewRouter(transport, auth.Identity{Subject: "admin", PlatformAdmin: true}, nil)

	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/prod/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body kube.Overview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Nodes.Total != 1 || body.Pods.Total != 1 || body.Namespaces.Total != 1 {
		t.Fatalf("overview = %+v", body)
	}
	if body.Scope.Namespaces != 1 {
		t.Fatalf("scope = %+v", body.Scope)
	}
}

func TestClusterOverviewUnknownCluster(t *testing.T) {
	router := overviewRouter(&fakeTransport{name: "prod"}, auth.Identity{Subject: "admin", PlatformAdmin: true}, nil)
	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/missing/overview")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", body.Code)
	}
}

func TestClusterOverviewForbiddenWithoutBinding(t *testing.T) {
	router := overviewRouter(&fakeTransport{name: "prod"}, auth.Identity{UserID: 7, Subject: "user"}, nil)
	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/prod/overview")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeForbidden {
		t.Fatalf("code = %s, want FORBIDDEN", body.Code)
	}
}

func TestClusterOverviewScopesToBoundNamespaces(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
	var listedMu sync.Mutex
	var listed []string
	transport := &fakeTransport{
		name:       "prod",
		namespaces: []string{"default", "kube-system", "other"},
		listFn: func(gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
			listedMu.Lock()
			listed = append(listed, gvr.Resource+"/"+namespace)
			listedMu.Unlock()
			if namespace == "" {
				return nil, forbidden
			}
			if gvr.Resource == "pods" && namespace == "default" {
				return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: overviewPod("p1")}}}, nil
			}
			return &unstructured.UnstructuredList{}, nil
		},
	}
	router := overviewRouter(transport, auth.Identity{UserID: 1, Subject: "user"},
		[]domain.RoleBinding{{UserID: 1, ClusterName: "prod", Namespace: "default", RoleCode: "viewer"}})

	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/prod/overview")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body kube.Overview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Pods.Total != 1 {
		t.Fatalf("pods = %+v, want only the bound namespace", body.Pods)
	}
	if body.Namespaces.Total != 1 || body.Scope.Namespaces != 1 {
		t.Fatalf("namespaces = %+v scope = %+v, want only the bound namespace", body.Namespaces, body.Scope)
	}
	if transport.namespaceCalls != 0 {
		t.Fatalf("namespace enumeration calls = %d, want 0 (bindings provide the scope)", transport.namespaceCalls)
	}
	listedMu.Lock()
	defer listedMu.Unlock()
	for _, call := range listed {
		switch call {
		case "nodes/", "events/", "pods/", "deployments/", "statefulsets/", "daemonsets/",
			"pods/default", "deployments/default", "statefulsets/default", "daemonsets/default", "events/default":
			continue
		default:
			t.Fatalf("unexpected list call %q; counts must not touch unbound namespaces", call)
		}
	}
}

func TestClusterOverviewUnreachable(t *testing.T) {
	transport := &fakeTransport{name: "prod"}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport, status: kube.StatusUnreachable}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{Subject: "admin", PlatformAdmin: true}},
	})
	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/prod/overview")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeClusterUnreachable {
		t.Fatalf("code = %s, want CLUSTER_UNREACHABLE", body.Code)
	}
}

func TestClusterOverviewDegraded(t *testing.T) {
	upstream := errors.New("upstream unavailable")
	transport := &fakeTransport{
		name: "prod",
		err:  upstream,
		listFn: func(schema.GroupVersionResource, string) (*unstructured.UnstructuredList, error) {
			return nil, apierrors.NewInternalError(upstream)
		},
	}
	router := overviewRouter(transport, auth.Identity{Subject: "admin", PlatformAdmin: true}, nil)
	rec := doRequest(t, router, http.MethodGet, "/api/v1/clusters/prod/overview")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeClusterDegraded {
		t.Fatalf("code = %s, want CLUSTER_DEGRADED", body.Code)
	}
}
