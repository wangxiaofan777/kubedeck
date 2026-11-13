package kube

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

// overviewTransport 是总览聚合测试用的可编程 Transport。
type overviewTransport struct {
	namespaces    []string
	namespacesErr error
	listFn        func(schema.GroupVersionResource, string, ListOptions) (*unstructured.UnstructuredList, error)
}

func (f *overviewTransport) Name() string { return "test" }
func (f *overviewTransport) Info(context.Context) ClusterInfo {
	return ClusterInfo{Name: "test", Status: StatusHealthy}
}
func (f *overviewTransport) Namespaces(context.Context) ([]string, error) {
	return f.namespaces, f.namespacesErr
}
func (f *overviewTransport) List(_ context.Context, gvr schema.GroupVersionResource, namespace string, opts ListOptions) (*unstructured.UnstructuredList, error) {
	return f.listFn(gvr, namespace, opts)
}
func (f *overviewTransport) Get(context.Context, schema.GroupVersionResource, string, string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}
func (f *overviewTransport) ReviewAccess(context.Context, AccessRequest) (AccessResult, error) {
	return AccessResult{}, nil
}
func (f *overviewTransport) Create(context.Context, schema.GroupVersionResource, string, *unstructured.Unstructured, []string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}
func (f *overviewTransport) Update(context.Context, schema.GroupVersionResource, string, *unstructured.Unstructured, []string) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}
func (f *overviewTransport) Delete(context.Context, schema.GroupVersionResource, string, string, DeleteOptions) error {
	return errors.New("not implemented")
}
func (f *overviewTransport) Patch(context.Context, schema.GroupVersionResource, string, string, types.PatchType, []byte, PatchOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}
func (f *overviewTransport) Scale(context.Context, schema.GroupVersionResource, string, string, int32) (*unstructured.Unstructured, error) {
	return nil, errors.New("not implemented")
}
func (f *overviewTransport) Evict(context.Context, string, string, *int64) error {
	return errors.New("not implemented")
}
func (f *overviewTransport) Logs(context.Context, string, string, LogOptions) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}
func (f *overviewTransport) Watch(context.Context, schema.GroupVersionResource, string, WatchOptions) (watch.Interface, error) {
	return nil, errors.New("not implemented")
}

func listOf(items ...map[string]any) *unstructured.UnstructuredList {
	out := &unstructured.UnstructuredList{}
	for _, item := range items {
		out.Items = append(out.Items, unstructured.Unstructured{Object: item})
	}
	return out
}

func nodeObject(name string, ready bool, cpu, memory string) map[string]any {
	status := "False"
	if ready {
		status = "True"
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Node",
		"metadata":   map[string]any{"name": name},
		"status": map[string]any{
			"conditions":  []any{map[string]any{"type": "Ready", "status": status}},
			"allocatable": map[string]any{"cpu": cpu, "memory": memory},
		},
	}
}

func podObject(name, phase string, crashLoop bool) map[string]any {
	status := map[string]any{"phase": phase}
	if crashLoop {
		status["containerStatuses"] = []any{map[string]any{
			"state": map[string]any{
				"waiting": map[string]any{"reason": "CrashLoopBackOff"},
			},
		}}
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"status":     status,
	}
}

func deploymentObject(name string, desired, ready int64) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"spec":       map[string]any{"replicas": desired},
		"status":     map[string]any{"readyReplicas": ready},
	}
}

func statefulSetObject(name string, desired, ready int64) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "StatefulSet",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"spec":       map[string]any{"replicas": desired},
		"status":     map[string]any{"readyReplicas": ready},
	}
}

func daemonSetObject(name string, desired, ready int64) map[string]any {
	return map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "DaemonSet",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
		"status":     map[string]any{"desiredNumberScheduled": desired, "numberReady": ready},
	}
}

func eventObject(namespace, name, lastTimestamp, message string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Event",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"type":       "Warning",
		"reason":     "BackOff",
		"message":    message,
		"involvedObject": map[string]any{
			"kind": "Pod",
			"name": name,
		},
		"lastTimestamp": lastTimestamp,
	}
}

func TestCollectOverviewSummarizes(t *testing.T) {
	longMessage := strings.Repeat("警", 250)
	transport := &overviewTransport{
		namespaces: []string{"default"},
		listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			switch gvr.Resource {
			case "nodes":
				return listOf(
					nodeObject("n1", true, "4", "8Gi"),
					nodeObject("n2", true, "2000m", "2Gi"),
					nodeObject("n3", false, "not-a-quantity", ""),
				), nil
			case "pods":
				return listOf(
					podObject("p1", "Running", true),
					podObject("p2", "Pending", false),
					podObject("p3", "Succeeded", false),
					podObject("p4", "Failed", false),
					podObject("p5", "", false),
				), nil
			case "deployments":
				return listOf(
					deploymentObject("d1", 3, 2),
					deploymentObject("d2", 2, 2),
				), nil
			case "statefulsets":
				return listOf(statefulSetObject("s1", 1, 1)), nil
			case "daemonsets":
				return listOf(daemonSetObject("ds1", 3, 2)), nil
			case "events":
				return listOf(
					eventObject("default", "e-old", "2026-11-13T09:00:00Z", "old"),
					eventObject("default", "e-new", "2026-11-13T11:00:00Z", longMessage),
					eventObject("default", "e-mid", "2026-11-13T10:00:00Z", "mid"),
				), nil
			default:
				return listOf(), nil
			}
		},
	}

	overview, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview: %v", err)
	}
	if overview.Nodes.Total != 3 || overview.Nodes.Ready != 2 {
		t.Fatalf("nodes = %+v, want total 3 ready 2", overview.Nodes)
	}
	if overview.Nodes.Allocatable.CPUMilli != 6000 {
		t.Fatalf("cpuMilli = %d, want 6000", overview.Nodes.Allocatable.CPUMilli)
	}
	if overview.Nodes.Allocatable.MemoryBytes != 10*1024*1024*1024 {
		t.Fatalf("memoryBytes = %d, want 10Gi", overview.Nodes.Allocatable.MemoryBytes)
	}
	if overview.Namespaces.Total != 1 || overview.Scope.Namespaces != 1 {
		t.Fatalf("namespaces = %+v scope = %+v", overview.Namespaces, overview.Scope)
	}
	want := OverviewPods{Total: 5, Running: 1, Pending: 1, Succeeded: 1, Failed: 1, Unknown: 1, CrashLoop: 1}
	if overview.Pods != want {
		t.Fatalf("pods = %+v, want %+v", overview.Pods, want)
	}
	if overview.Workloads.Deployments != (WorkloadCount{Total: 2, Unavailable: 1}) {
		t.Fatalf("deployments = %+v", overview.Workloads.Deployments)
	}
	if overview.Workloads.StatefulSets != (WorkloadCount{Total: 1, Unavailable: 0}) {
		t.Fatalf("statefulsets = %+v", overview.Workloads.StatefulSets)
	}
	if overview.Workloads.DaemonSets != (WorkloadCount{Total: 1, Unavailable: 1}) {
		t.Fatalf("daemonsets = %+v", overview.Workloads.DaemonSets)
	}
	if len(overview.Events) != 3 {
		t.Fatalf("events = %d, want 3", len(overview.Events))
	}
	if overview.Events[0].Object != "Pod/e-new" || overview.Events[2].Object != "Pod/e-old" {
		t.Fatalf("events order = %+v", overview.Events)
	}
	if got := len([]rune(overview.Events[0].Message)); got != overviewEventMessageRunes {
		t.Fatalf("message runes = %d, want %d", got, overviewEventMessageRunes)
	}
	if len(overview.Unavailable) != 0 {
		t.Fatalf("unavailable = %v, want empty", overview.Unavailable)
	}
	if !overview.Scope.AllNamespaces || overview.Scope.Truncated {
		t.Fatalf("scope = %+v, want allNamespaces true and not truncated", overview.Scope)
	}
}

func TestCollectOverviewFallsBackToVisibleNamespaces(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
	transport := &overviewTransport{
		namespaces: []string{"default", "kube-system"},
		listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			if gvr.Resource == "nodes" {
				return listOf(nodeObject("n1", true, "4", "8Gi")), nil
			}
			if namespace == "" {
				return nil, forbidden
			}
			if namespace == "kube-system" {
				return nil, forbidden
			}
			switch gvr.Resource {
			case "pods":
				return listOf(podObject("p1", "Running", false)), nil
			case "deployments":
				return listOf(deploymentObject("d1", 1, 1)), nil
			case "events":
				return listOf(eventObject("default", "e1", "2026-11-13T10:00:00Z", "ok")), nil
			default:
				return listOf(), nil
			}
		},
	}

	overview, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview: %v", err)
	}
	if overview.Pods.Total != 1 || overview.Pods.Running != 1 {
		t.Fatalf("pods = %+v, want only default namespace", overview.Pods)
	}
	if overview.Workloads.Deployments.Total != 1 {
		t.Fatalf("deployments = %+v, want only default namespace", overview.Workloads.Deployments)
	}
	if len(overview.Events) != 1 {
		t.Fatalf("events = %+v, want only default namespace", overview.Events)
	}
	if overview.Scope.AllNamespaces {
		t.Fatal("scope.allNamespaces = true, want false after fallback")
	}
	if len(overview.Unavailable) != 0 {
		t.Fatalf("unavailable = %v, want empty", overview.Unavailable)
	}
}

func TestCollectOverviewSkipsForbiddenNamespacesWithoutLeaking(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
	transport := &overviewTransport{
		namespaces: []string{"default", "secret-ns"},
		listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			switch {
			case gvr.Resource == "nodes":
				return listOf(), nil
			case namespace == "":
				return nil, forbidden
			case namespace == "secret-ns":
				return nil, forbidden
			case gvr.Resource == "pods":
				return listOf(podObject("p1", "Running", false)), nil
			default:
				return listOf(), nil
			}
		},
	}

	overview, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview: %v", err)
	}
	if overview.Pods.Total != 1 {
		t.Fatalf("pods = %+v, want only the visible namespace", overview.Pods)
	}
	if overview.Scope.Namespaces != 2 {
		t.Fatalf("scope.namespaces = %d, want 2", overview.Scope.Namespaces)
	}
}

func TestCollectOverviewTruncatesAtLimit(t *testing.T) {
	items := make([]map[string]any, 0, overviewPageSize)
	for i := 0; i < overviewPageSize; i++ {
		items = append(items, podObject("p"+time.Now().String(), "Running", false))
	}
	transport := &overviewTransport{
		namespaces: []string{"default"},
		listFn: func(gvr schema.GroupVersionResource, _ string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			switch gvr.Resource {
			case "pods":
				out := listOf(items...)
				out.SetContinue("next")
				return out, nil
			default:
				return listOf(), nil
			}
		},
	}

	overview, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview: %v", err)
	}
	if overview.Pods.Total != overviewMaxObjects {
		t.Fatalf("pods.total = %d, want %d", overview.Pods.Total, overviewMaxObjects)
	}
	if !overview.Scope.Truncated {
		t.Fatal("scope.truncated = false, want true")
	}
}

func TestCollectOverviewPartialFailure(t *testing.T) {
	transport := &overviewTransport{
		namespaces: []string{"default"},
		listFn: func(gvr schema.GroupVersionResource, _ string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			if gvr.Resource == "events" {
				return nil, apierrors.NewInternalError(errors.New("events unavailable"))
			}
			switch gvr.Resource {
			case "nodes":
				return listOf(nodeObject("n1", true, "4", "8Gi")), nil
			case "pods":
				return listOf(podObject("p1", "Running", false)), nil
			default:
				return listOf(), nil
			}
		},
	}

	overview, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{})
	if err != nil {
		t.Fatalf("CollectOverview: %v", err)
	}
	if overview.Nodes.Total != 1 || overview.Pods.Total != 1 {
		t.Fatalf("overview = %+v, want nodes and pods collected", overview)
	}
	if len(overview.Events) != 0 {
		t.Fatalf("events = %+v, want empty", overview.Events)
	}
	if !containsString(overview.Unavailable, "events") {
		t.Fatalf("unavailable = %v, want events", overview.Unavailable)
	}
}

func TestCollectOverviewDegradedWhenEverythingFails(t *testing.T) {
	transport := &overviewTransport{
		namespacesErr: errors.New("namespaces unavailable"),
		listFn: func(schema.GroupVersionResource, string, ListOptions) (*unstructured.UnstructuredList, error) {
			return nil, apierrors.NewInternalError(errors.New("upstream failure"))
		},
	}

	_, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{})
	if !errors.Is(err, ErrOverviewDegraded) {
		t.Fatalf("err = %v, want ErrOverviewDegraded", err)
	}
}

func TestCollectOverviewBoundsNamespaceParallelism(t *testing.T) {
	namespaces := make([]string, 20)
	for i := range namespaces {
		namespaces[i] = "ns-" + string(rune('a'+i))
	}
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("denied"))
	var inflight atomic.Int32
	var peak atomic.Int32
	var mu sync.Mutex
	namespaceCalls := 0

	transport := &overviewTransport{
		namespaces: namespaces,
		listFn: func(gvr schema.GroupVersionResource, namespace string, _ ListOptions) (*unstructured.UnstructuredList, error) {
			if namespace == "" {
				return nil, forbidden
			}
			mu.Lock()
			namespaceCalls++
			mu.Unlock()
			current := inflight.Add(1)
			defer inflight.Add(-1)
			for {
				peakNow := peak.Load()
				if current <= peakNow || peak.CompareAndSwap(peakNow, current) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			return listOf(), nil
		},
	}

	if _, err := CollectOverview(context.Background(), "prod", transport, OverviewOptions{}); err != nil {
		t.Fatalf("CollectOverview: %v", err)
	}
	if peak.Load() > overviewParallelism {
		t.Fatalf("peak parallelism = %d, want <= %d", peak.Load(), overviewParallelism)
	}
	if peak.Load() < 2 {
		t.Fatalf("peak parallelism = %d, want concurrent calls", peak.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if namespaceCalls == 0 {
		t.Fatal("no per-namespace calls made")
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
