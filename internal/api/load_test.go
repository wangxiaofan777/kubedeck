package api

import (
	"context"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

const (
	loadTestPods  = 50000
	loadTestNodes = 1000
	loadTestPage  = 500
	loadTestRuns  = 100
)

// pagingTransport 模拟 apiserver 的 limit/continue 分页：从 5 万对象中只返回一页。
type pagingTransport struct {
	*fakeTransport
	objects []unstructured.Unstructured
}

func (p *pagingTransport) List(_ context.Context, _ schema.GroupVersionResource, _ string, opts kube.ListOptions) (*unstructured.UnstructuredList, error) {
	start := 0
	if opts.Continue != "" {
		start, _ = strconv.Atoi(opts.Continue)
	}
	end := start + int(opts.Limit)
	if end > len(p.objects) {
		end = len(p.objects)
	}
	list := &unstructured.UnstructuredList{Items: p.objects[start:end]}
	if end < len(p.objects) {
		list.SetContinue(strconv.Itoa(end))
	}
	remaining := int64(len(p.objects) - end)
	list.SetRemainingItemCount(&remaining)
	list.SetResourceVersion("50")
	return list, nil
}

type loadCluster struct {
	name      string
	transport kube.Transport
}

func (c *loadCluster) Name() string { return c.name }
func (c *loadCluster) Info(context.Context) kube.ClusterInfo {
	return kube.ClusterInfo{Name: c.name, Version: "v1.37.2", Status: kube.StatusHealthy}
}
func (c *loadCluster) TransportFor(kube.UserRef) (kube.Transport, error) { return c.transport, nil }
func (c *loadCluster) Discovery(context.Context) (kube.DiscoveryResult, error) {
	return kube.DiscoveryResult{Resources: []kube.APIResource{
		{Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true},
		{Version: "v1", Resource: "nodes", Kind: "Node", Namespaced: false},
	}}, nil
}

// TestScaleListP95 用 1000 节点 / 50k Pod 的模拟分页数据测量列表接口 P95。
// 只覆盖 Kubedeck 自身处理与序列化一页 500 条的成本；apiserver 侧的全量扫描不在范围内。
func TestScaleListP95(t *testing.T) {
	objects := make([]unstructured.Unstructured, 0, loadTestPods)
	for i := 0; i < loadTestPods; i++ {
		ns := "ns-" + strconv.Itoa(i%50)
		objects = append(objects, unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":            "pod-" + strconv.Itoa(i),
				"namespace":       ns,
				"uid":             "uid-" + strconv.Itoa(i),
				"resourceVersion": strconv.Itoa(i + 100),
			},
			"status": map[string]any{"phase": "Running"},
		}})
	}
	// 1000 个节点与 5 万 Pod 同时驻留，确认规模数据共存时列表页成本仍由单页大小决定。
	nodes := make([]unstructured.Unstructured, loadTestNodes)
	for i := 0; i < loadTestNodes; i++ {
		nodes[i] = unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Node",
			"metadata":   map[string]any{"name": "node-" + strconv.Itoa(i)},
		}}
	}
	if len(nodes) != loadTestNodes {
		t.Fatalf("nodes = %d", len(nodes))
	}

	var mem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mem)
	t.Logf("simulated objects heap=%d MiB", mem.HeapAlloc/(1<<20))

	transport := &pagingTransport{fakeTransport: &fakeTransport{name: "prod"}, objects: objects}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &loadCluster{name: "prod", transport: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth: fakeAuth{authenticated: true, identity: auth.Identity{
			UserID: 1, Subject: "load", PlatformAdmin: true,
		}},
	})

	durations := make([]time.Duration, 0, loadTestRuns)
	for i := 0; i < loadTestRuns; i++ {
		start := time.Now()
		rec := doRequest(t, router, http.MethodGet,
			"/api/v1/clusters/prod/resources/_/v1/pods?namespace=ns-1&limit="+strconv.Itoa(loadTestPage))
		durations = append(durations, time.Since(start))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() == 0 {
			t.Fatal("empty response body")
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	t.Logf("pods=%d nodes=%d page=%d runs=%d p95=%v max=%v",
		loadTestPods, loadTestNodes, loadTestPage, loadTestRuns, p95, durations[len(durations)-1])
	if p95 > 300*time.Millisecond {
		t.Fatalf("list p95 = %v, want < 300ms", p95)
	}
}
