package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

func TestParseLogOptionsRejectsOutOfRange(t *testing.T) {
	cases := []string{
		"/api/v1/clusters/prod/pods/default/p/log?tailLines=99999",
		"/api/v1/clusters/prod/pods/default/p/log?sinceSeconds=-1",
		"/api/v1/clusters/prod/pods/default/p/log?sinceTime=yesterday",
	}
	for _, target := range cases {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if _, err := parseLogOptions(r); err == nil {
			t.Fatalf("%s should be rejected", target)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/pods/default/p/log?tailLines=20&previous=true", nil)
	opts, err := parseLogOptions(r)
	if err != nil {
		t.Fatal(err)
	}
	if opts.TailLines == nil || *opts.TailLines != 20 || !opts.Previous || !opts.Timestamps {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestListEventsAggregatesDuplicates(t *testing.T) {
	events := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		{Object: map[string]any{
			"metadata": map[string]any{"name": "e1", "namespace": "default"},
			"type":     "Warning", "reason": "BackOff", "message": "restarting",
			"count":          int64(2),
			"lastTimestamp":  "2026-01-01T00:00:02Z",
			"involvedObject": map[string]any{"kind": "Pod", "name": "web", "uid": "u1"},
		}},
		{Object: map[string]any{
			"metadata": map[string]any{"name": "e2", "namespace": "default"},
			"type":     "Warning", "reason": "BackOff", "message": "restarting",
			"count":          int64(3),
			"lastTimestamp":  "2026-01-01T00:00:05Z",
			"involvedObject": map[string]any{"kind": "Pod", "name": "web", "uid": "u1"},
		}},
		{Object: map[string]any{
			"metadata": map[string]any{"name": "e3", "namespace": "default"},
			"type":     "Normal", "reason": "Pulled", "message": "image pulled",
			"count":          int64(1),
			"lastTimestamp":  "2026-01-01T00:00:01Z",
			"involvedObject": map[string]any{"kind": "Pod", "name": "web", "uid": "u1"},
		}},
	}}
	transport := &fakeTransport{name: "prod", list: events, object: &unstructured.Unstructured{}}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/clusters/prod/events?namespace=default&kind=Pod&name=web", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %+v, want 2 aggregated events", resp.Items)
	}
	if resp.Items[0]["reason"] != "BackOff" || resp.Items[0]["count"].(float64) != 3 {
		t.Fatalf("top event = %+v, want BackOff count 3", resp.Items[0])
	}
}

func TestPodLogTruncatesAtLimit(t *testing.T) {
	transport := &fakeTransport{
		name:    "prod",
		logBody: strings.Repeat("x", maxLogBytes+1024),
		object: &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"containers": []any{map[string]any{"name": "web"}}},
		}},
	}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{Subject: "tester", PlatformAdmin: true}},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/prod/pods/default/web/log", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Log-Truncated") != "true" {
		prefix := rec.Body.String()
		if len(prefix) > 200 {
			prefix = prefix[:200]
		}
		t.Fatalf("truncated header missing; headers=%v bodyLen=%d body=%q", rec.Header(), rec.Body.Len(), prefix)
	}
	if rec.Body.Len() != maxLogBytes {
		t.Fatalf("body len = %d, want %d", rec.Body.Len(), maxLogBytes)
	}
}
