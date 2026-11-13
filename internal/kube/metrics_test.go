package kube

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func nodeMetricsObject(name, cpu, memory string) map[string]any {
	return map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "NodeMetrics",
		"metadata":   map[string]any{"name": name},
		"timestamp":  "2026-11-13T08:30:00Z",
		"usage":      map[string]any{"cpu": cpu, "memory": memory},
	}
}

func podMetricsObject(namespace, name string, containers ...map[string]any) map[string]any {
	items := make([]any, 0, len(containers))
	for _, container := range containers {
		items = append(items, container)
	}
	return map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "PodMetrics",
		"metadata":   map[string]any{"namespace": namespace, "name": name},
		"timestamp":  "2026-11-13T08:30:00Z",
		"containers": items,
	}
}

func containerMetrics(cpu, memory string) map[string]any {
	return map[string]any{
		"name":  "app",
		"usage": map[string]any{"cpu": cpu, "memory": memory},
	}
}

func forbiddenError(resource string) error {
	return apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: resource}, "", errors.New("denied"))
}

func TestCollectUsageNodes(t *testing.T) {
	tr := &overviewTransport{listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
		if gvr != nodeMetricsGVR {
			return nil, errors.New("unexpected resource: " + gvr.String())
		}
		return listOf(nodeMetricsObject("n1", "250m", "1Gi"), nodeMetricsObject("n2", "150m", "512Mi")), nil
	}}

	usage, err := CollectUsage(context.Background(), tr, OverviewOptions{}, make(chan struct{}, overviewParallelism))
	if err != nil {
		t.Fatalf("CollectUsage() error = %v", err)
	}
	if !usage.Available || usage.Scope != UsageScopeNodes {
		t.Fatalf("usage = %+v, want available nodes", usage)
	}
	if usage.CPUMilli != 400 {
		t.Errorf("cpuMilli = %d, want 400", usage.CPUMilli)
	}
	if want := int64(1536 * 1024 * 1024); usage.MemoryBytes != want {
		t.Errorf("memoryBytes = %d, want %d", usage.MemoryBytes, want)
	}
	if usage.CPUAllocatableMilli != nil || usage.CPUPercent != nil {
		t.Errorf("capacity should be filled by overview, got %+v", usage)
	}
	if usage.FetchedAt.IsZero() {
		t.Error("fetchedAt is zero")
	}
}

func TestCollectUsagePodsScoped(t *testing.T) {
	tr := &overviewTransport{listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
		if gvr != podMetricsGVR {
			return nil, errors.New("unexpected resource: " + gvr.String())
		}
		switch namespace {
		case "default":
			return listOf(
				podMetricsObject("default", "web", containerMetrics("100m", "128Mi"), containerMetrics("50m", "64Mi")),
			), nil
		case "dev":
			return nil, forbiddenError("pods")
		}
		return nil, errors.New("unexpected namespace: " + namespace)
	}}

	opts := OverviewOptions{Namespaces: []string{"default", "dev"}, NamespacesKnown: true}
	usage, err := CollectUsage(context.Background(), tr, opts, make(chan struct{}, overviewParallelism))
	if err != nil {
		t.Fatalf("CollectUsage() error = %v", err)
	}
	if usage.Scope != UsageScopePods {
		t.Fatalf("scope = %q, want %q", usage.Scope, UsageScopePods)
	}
	if usage.CPUMilli != 150 {
		t.Errorf("cpuMilli = %d, want 150", usage.CPUMilli)
	}
	if want := int64(192 * 1024 * 1024); usage.MemoryBytes != want {
		t.Errorf("memoryBytes = %d, want %d", usage.MemoryBytes, want)
	}
	if usage.CPUPercent != nil || usage.CPUAllocatableMilli != nil {
		t.Errorf("scoped usage must not expose capacity, got %+v", usage)
	}
}

func TestCollectUsageFallsBackWhenNodeMetricsForbidden(t *testing.T) {
	tr := &overviewTransport{listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
		switch gvr {
		case nodeMetricsGVR:
			return nil, forbiddenError("nodes")
		case podMetricsGVR:
			return listOf(podMetricsObject("default", "web", containerMetrics("80m", "64Mi"))), nil
		}
		return nil, errors.New("unexpected resource: " + gvr.String())
	}}

	usage, err := CollectUsage(context.Background(), tr, OverviewOptions{}, make(chan struct{}, overviewParallelism))
	if err != nil {
		t.Fatalf("CollectUsage() error = %v", err)
	}
	if usage.Scope != UsageScopePods {
		t.Fatalf("scope = %q, want pod fallback", usage.Scope)
	}
	if usage.CPUMilli != 80 {
		t.Errorf("cpuMilli = %d, want 80", usage.CPUMilli)
	}
}

func TestCollectUsageMetricsAPIMissing(t *testing.T) {
	tr := &overviewTransport{listFn: func(gvr schema.GroupVersionResource, _ string, _ ListOptions) (*unstructured.UnstructuredList, error) {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "nodes"}, "")
	}}

	usage, err := CollectUsage(context.Background(), tr, OverviewOptions{}, make(chan struct{}, overviewParallelism))
	if !errors.Is(err, ErrMetricsUnavailable) || usage != nil {
		t.Fatalf("usage = %+v, err = %v, want ErrMetricsUnavailable", usage, err)
	}
}

func TestCollectUsageEmptyMetrics(t *testing.T) {
	tr := &overviewTransport{listFn: func(schema.GroupVersionResource, string, ListOptions) (*unstructured.UnstructuredList, error) {
		return listOf(), nil
	}}

	usage, err := CollectUsage(context.Background(), tr, OverviewOptions{}, make(chan struct{}, overviewParallelism))
	if err != nil {
		t.Fatalf("CollectUsage() error = %v", err)
	}
	if !usage.Available || usage.CPUMilli != 0 || usage.MemoryBytes != 0 {
		t.Fatalf("usage = %+v, want available zero", usage)
	}
	if usage.FetchedAt.IsZero() {
		t.Error("fetchedAt is zero for empty metrics")
	}
}

func TestOverviewFillsUsageCapacity(t *testing.T) {
	tr := &overviewTransport{
		namespaces: []string{"default"},
		listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			switch gvr {
			case overviewNodesGVR:
				return listOf(nodeObject("n1", true, "2", "4Gi")), nil
			case nodeMetricsGVR:
				return listOf(nodeMetricsObject("n1", "500m", "1Gi")), nil
			case overviewPodsGVR, overviewDeploymentsGVR, overviewStatefulSetsGVR, overviewDaemonSetsGVR, overviewEventsGVR:
				return listOf(), nil
			}
			return nil, errors.New("unexpected resource: " + gvr.String())
		},
	}

	overview, err := CollectOverview(context.Background(), "local", tr, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview() error = %v", err)
	}
	if overview.Usage == nil {
		t.Fatal("usage is nil")
	}
	if overview.Usage.CPUPercent == nil || *overview.Usage.CPUPercent != 25 {
		t.Errorf("cpuPercent = %v, want 25", overview.Usage.CPUPercent)
	}
	if overview.Usage.MemoryPercent == nil || *overview.Usage.MemoryPercent != 25 {
		t.Errorf("memoryPercent = %v, want 25", overview.Usage.MemoryPercent)
	}
	if overview.Usage.CPUAllocatableMilli == nil || *overview.Usage.CPUAllocatableMilli != 2000 {
		t.Errorf("cpuAllocatableMilli = %v, want 2000", overview.Usage.CPUAllocatableMilli)
	}
	for _, item := range overview.Unavailable {
		if item == "metrics" {
			t.Fatal("metrics marked unavailable although it succeeded")
		}
	}
}

func TestOverviewDegradesMetricsFailure(t *testing.T) {
	tr := &overviewTransport{
		namespaces: []string{"default"},
		listFn: func(gvr schema.GroupVersionResource, _ string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			switch gvr {
			case overviewNodesGVR:
				return listOf(nodeObject("n1", true, "2", "4Gi")), nil
			case nodeMetricsGVR:
				return nil, apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "nodes"}, "")
			case overviewPodsGVR, overviewDeploymentsGVR, overviewStatefulSetsGVR, overviewDaemonSetsGVR, overviewEventsGVR:
				return listOf(), nil
			}
			return nil, errors.New("unexpected resource: " + gvr.String())
		},
	}

	overview, err := CollectOverview(context.Background(), "local", tr, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview() error = %v", err)
	}
	if overview.Usage != nil {
		t.Fatalf("usage = %+v, want nil", overview.Usage)
	}
	found := false
	for _, item := range overview.Unavailable {
		if item == "metrics" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unavailable = %v, want metrics", overview.Unavailable)
	}
	if overview.Nodes.Total != 1 {
		t.Errorf("nodes.total = %d, want 1", overview.Nodes.Total)
	}
}
