package api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseYAMLObjectPreservesIntegers(t *testing.T) {
	obj, err := parseYAMLObject([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: web
        resources:
          limits:
            cpu: 0.5
`))
	if err != nil {
		t.Fatal(err)
	}
	replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if err != nil || !found || replicas != 3 {
		t.Fatalf("replicas = %v found=%v err=%v", replicas, found, err)
	}
}

func TestImmutableWarningsDetectSelectorChange(t *testing.T) {
	current := &unstructured.Unstructured{Object: map[string]any{
		"kind": "Deployment",
		"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": "web"}}},
	}}
	submitted := current.DeepCopy()
	_ = unstructured.SetNestedField(submitted.Object, "other", "spec", "selector", "matchLabels", "app")
	warnings := immutableWarnings(current, submitted)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "selector") {
		t.Fatalf("warnings = %v", warnings)
	}

	same := current.DeepCopy()
	if got := immutableWarnings(current, same); len(got) != 0 {
		t.Fatalf("unchanged selector warnings = %v", got)
	}
}

func TestValidObjectNameRejectsTraversal(t *testing.T) {
	cases := []string{"", "..", "a/b", "a\nb", strings.Repeat("a", 254)}
	for _, name := range cases {
		rec := httptest.NewRecorder()
		if validObjectName(rec, context.Background(), name) {
			t.Fatalf("name %q should be rejected", name)
		}
	}
	rec := httptest.NewRecorder()
	if !validObjectName(rec, context.Background(), "web-7bfb4bc78d-mjgrh") {
		t.Fatal("valid DNS name rejected")
	}
}

func TestUnifiedDiffShowsImageChange(t *testing.T) {
	before := "spec:\n  template:\n    spec:\n      containers:\n      - image: nginx:1.25\n"
	after := "spec:\n  template:\n    spec:\n      containers:\n      - image: nginx:1.26\n"
	diff := unifiedDiff(before, after)
	if !strings.Contains(diff, "-      - image: nginx:1.25") || !strings.Contains(diff, "+      - image: nginx:1.26") {
		t.Fatalf("diff = %q", diff)
	}
}
