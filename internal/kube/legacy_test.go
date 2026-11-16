package kube

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

// TestLegacy118Integration 验证 KubeDeck 在 Kubernetes 1.18 上的核心路径。
// 需要一台 1.18.x 测试集群与 kubeconfig；未设置环境变量时跳过，不阻塞常规流水线。
//
//	KUBEDECK_TEST_LEGACY_KUBECONFIG=.testenv/k3s-118.yaml go test ./internal/kube/ -run Legacy -v
func TestLegacy118Integration(t *testing.T) {
	path := os.Getenv("KUBEDECK_TEST_LEGACY_KUBECONFIG")
	if path == "" {
		t.Skip("KUBEDECK_TEST_LEGACY_KUBECONFIG not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cluster, err := NewDirectClusterFromKubeconfig("legacy", path)
	if err != nil {
		t.Fatalf("load kubeconfig: %v", err)
	}
	info := cluster.Probe(ctx)
	if info.Status != StatusHealthy {
		t.Fatalf("probe status = %s (%s)", info.Status, info.Message)
	}
	t.Logf("cluster version: %s", info.Version)

	discovery := assertLegacyDiscovery(t, ctx, cluster)

	tr, err := cluster.TransportFor(UserRef{})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	ns := "default"

	// ConfigMap CRUD 覆盖 dynamic client 的主路径。
	cms := schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	name := fmt.Sprintf("kd-legacy-%d", time.Now().UnixNano())
	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"data":       map[string]any{"step": "create"},
	}}
	if _, err := tr.Create(ctx, cms, ns, cm, nil); err != nil {
		t.Fatalf("create configmap: %v", err)
	}
	t.Cleanup(func() { _ = tr.Delete(context.Background(), cms, ns, name, DeleteOptions{}) })

	w, err := tr.Watch(ctx, cms, ns, WatchOptions{ResourceVersion: "0"})
	if err != nil {
		t.Fatalf("watch configmap: %v", err)
	}
	defer w.Stop()

	if _, err := tr.Patch(ctx, cms, ns, name, types.MergePatchType,
		[]byte(`{"data":{"step":"patched","extra":"1"}}`), PatchOptions{}); err != nil {
		t.Fatalf("patch configmap: %v", err)
	}

	// watch 事件必须能到达，证明 1.18 的 list/watch 握手与 0.37 客户端兼容。
	deadline := time.After(20 * time.Second)
	seen := false
	for !seen {
		select {
		case ev, ok := <-w.ResultChan():
			if !ok {
				t.Fatal("watch channel closed before event")
			}
			if ev.Type == watch.Added || ev.Type == watch.Modified {
				seen = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for watch event")
		}
	}

	got, err := tr.Get(ctx, cms, ns, name)
	if err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	if v, _, _ := unstructured.NestedString(got.Object, "data", "step"); v != "patched" {
		t.Fatalf("configmap step = %q, want patched", v)
	}
	// Update 走 resourceVersion 并发校验，1.18 与新版行为一致。
	got.Object["data"] = map[string]any{"step": "updated"}
	if _, err := tr.Update(ctx, cms, ns, got, nil); err != nil {
		t.Fatalf("update configmap: %v", err)
	}

	// Deployment scale 子资源在 1.18 使用 autoscaling/v1，与当前实现一致。
	deps := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	dep := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"spec": map[string]any{
			"replicas": int64(1),
			"selector": map[string]any{"matchLabels": map[string]any{"app": name}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": name}},
				"spec": map[string]any{"containers": []any{map[string]any{
					"name": "pause", "image": "registry.k8s.io/pause:3.9",
				}}},
			},
		},
	}}
	if _, err := tr.Create(ctx, deps, ns, dep, nil); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	t.Cleanup(func() { _ = tr.Delete(context.Background(), deps, ns, name, DeleteOptions{}) })
	scaled, err := tr.Scale(ctx, deps, ns, name, 2)
	if err != nil {
		t.Fatalf("scale deployment: %v", err)
	}
	replicas, _, _ := unstructured.NestedInt64(scaled.Object, "spec", "replicas")
	if replicas != 2 {
		t.Fatalf("replicas after scale = %d, want 2", replicas)
	}

	// 1.18 只有 policy/v1beta1：Evict 必须自动降级而不是返回 404。
	if discovery.policyV1 {
		t.Fatal("test cluster unexpectedly serves policy/v1; this test targets legacy clusters")
	}
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "namespace": ns},
		"spec": map[string]any{"containers": []any{map[string]any{
			"name": "pause", "image": "registry.k8s.io/pause:3.9",
		}}},
	}}
	if _, err := tr.Create(ctx, pods, ns, pod, nil); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	t.Cleanup(func() { _ = tr.Delete(context.Background(), pods, ns, name, DeleteOptions{}) })
	if err := tr.Evict(ctx, ns, name, nil); err != nil {
		t.Fatalf("evict pod via policy/v1beta1: %v", err)
	}

	direct := tr.(*DirectTransport)
	if direct.evictAPIVersion != "policy/v1beta1" {
		t.Fatalf("eviction api version = %q, want policy/v1beta1", direct.evictAPIVersion)
	}
}

type legacyDiscovery struct {
	policyV1 bool
}

// assertLegacyDiscovery 确认集群确实只提供 legacy API，并记录版本选择的前提。
func assertLegacyDiscovery(t *testing.T, ctx context.Context, c *DirectCluster) legacyDiscovery {
	t.Helper()
	result, err := c.Discovery(ctx)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	found := map[string]bool{}
	for _, r := range result.Resources {
		found[r.Group+"/"+r.Version+"/"+r.Resource] = true
	}
	t.Logf("discovered %d resources (failed groups: %v)", len(result.Resources), result.FailedGroups)

	for _, want := range []string{
		"batch/v1beta1/cronjobs",
		"networking.k8s.io/v1beta1/ingresses",
		"policy/v1beta1/poddisruptionbudgets",
	} {
		if !found[want] {
			t.Fatalf("legacy cluster does not serve %s", want)
		}
	}
	_, err = c.disc.ServerResourcesForGroupVersion("policy/v1")
	return legacyDiscovery{policyV1: err == nil}
}
