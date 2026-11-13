package kube

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ErrMetricsUnavailable 表示集群没有可用的指标 API，或指标读取整体失败。
// 总览把这种情况记为降级，而不是请求失败。
var ErrMetricsUnavailable = errors.New("kube: metrics unavailable")

// 用量口径：nodes 表示节点指标汇总（含容量），pods 表示可见命名空间内的 Pod 汇总。
const (
	UsageScopeNodes = "nodes"
	UsageScopePods  = "pods"
)

var (
	nodeMetricsGVR = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}
	podMetricsGVR  = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
)

// OverviewUsage 是总览的资源用量。容量与百分比只在节点口径下返回；
// 命名空间受限用户拿不到节点容量，对应字段为 null。
type OverviewUsage struct {
	Available              bool      `json:"available"`
	Scope                  string    `json:"scope"`
	FetchedAt              time.Time `json:"fetchedAt"`
	CPUMilli               int64     `json:"cpuMilli"`
	MemoryBytes            int64     `json:"memoryBytes"`
	CPUAllocatableMilli    *int64    `json:"cpuAllocatableMilli"`
	MemoryAllocatableBytes *int64    `json:"memoryAllocatableBytes"`
	CPUPercent             *float64  `json:"cpuPercent"`
	MemoryPercent          *float64  `json:"memoryPercent"`
}

// CollectUsage 以当前用户身份读取资源用量。
// 范围不受限时优先取节点指标；受限用户只汇总可见命名空间的 Pod 指标。
func CollectUsage(ctx context.Context, t Transport, opts OverviewOptions, sem chan struct{}) (*OverviewUsage, error) {
	now := time.Now().UTC()

	if !opts.NamespacesKnown {
		usage, err := collectNodeUsage(ctx, t, now)
		if err == nil {
			return usage, nil
		}
		// 只有节点指标被拒绝时才尝试 Pod 指标；指标 API 缺失或超时直接降级，
		// 避免在同一个不可用的 API 上重复请求。
		if !apierrors.IsForbidden(err) {
			return nil, ErrMetricsUnavailable
		}
	}

	usage, err := collectPodUsage(ctx, t, opts, sem, now)
	if err != nil {
		return nil, ErrMetricsUnavailable
	}
	return usage, nil
}

func collectNodeUsage(ctx context.Context, t Transport, now time.Time) (*OverviewUsage, error) {
	items, _, err := listAll(ctx, t, nodeMetricsGVR, "")
	if err != nil {
		return nil, err
	}
	var cpu, memory int64
	for i := range items {
		milli, ok := usageQuantityMilli(&items[i], "cpu")
		if ok {
			cpu += milli
		}
		value, ok := usageQuantityValue(&items[i], "memory")
		if ok {
			memory += value
		}
	}
	return &OverviewUsage{
		Available:   true,
		Scope:       UsageScopeNodes,
		FetchedAt:   metricTimestamp(items, now),
		CPUMilli:    cpu,
		MemoryBytes: memory,
	}, nil
}

func collectPodUsage(ctx context.Context, t Transport, opts OverviewOptions, sem chan struct{}, now time.Time) (*OverviewUsage, error) {
	var (
		items   []unstructured.Unstructured
		succeed int
	)
	if opts.NamespacesKnown {
		// 范围已知时不再先做一次必然被拒绝的集群级列表，直接按命名空间读取。
		items, succeed = listPerNamespace(ctx, t, podMetricsGVR, opts.Namespaces, sem)
		if len(opts.Namespaces) > 0 && succeed == 0 {
			return nil, ErrMetricsUnavailable
		}
	} else {
		listed, _, _, err := listScoped(ctx, t, podMetricsGVR, nil, false, sem)
		if err != nil {
			return nil, err
		}
		items = listed
	}
	var cpu, memory int64
	for i := range items {
		containers, found, err := unstructured.NestedSlice(items[i].Object, "containers")
		if err != nil || !found {
			continue
		}
		for _, container := range containers {
			entry, ok := container.(map[string]any)
			if !ok {
				continue
			}
			usage, ok := entry["usage"].(map[string]any)
			if !ok {
				continue
			}
			if milli, ok := quantityFromMap(usage, "cpu", true); ok {
				cpu += milli
			}
			if value, ok := quantityFromMap(usage, "memory", false); ok {
				memory += value
			}
		}
	}
	return &OverviewUsage{
		Available:   true,
		Scope:       UsageScopePods,
		FetchedAt:   metricTimestamp(items, now),
		CPUMilli:    cpu,
		MemoryBytes: memory,
	}, nil
}

// fillUsageCapacity 在节点统计可用时补齐容量与使用率。
// 节点统计失败或集群没有可分配容量时保持 null，前端据此隐藏百分比。
func (o *Overview) fillUsageCapacity() {
	if o.Usage == nil || o.Usage.Scope != UsageScopeNodes {
		return
	}
	if allocatable := o.Nodes.Allocatable.CPUMilli; allocatable > 0 {
		value := allocatable
		percent := ratioPercent(o.Usage.CPUMilli, allocatable)
		o.Usage.CPUAllocatableMilli = &value
		o.Usage.CPUPercent = &percent
	}
	if allocatable := o.Nodes.Allocatable.MemoryBytes; allocatable > 0 {
		value := allocatable
		percent := ratioPercent(o.Usage.MemoryBytes, allocatable)
		o.Usage.MemoryAllocatableBytes = &value
		o.Usage.MemoryPercent = &percent
	}
}

func ratioPercent(used, total int64) float64 {
	return math.Round(float64(used)/float64(total)*10000) / 100
}

// usageQuantityMilli 读取指标对象的 usage.<name>（CPU 用毫核）。
func usageQuantityMilli(item *unstructured.Unstructured, name string) (int64, bool) {
	usage, found, err := unstructured.NestedMap(item.Object, "usage")
	if err != nil || !found {
		return 0, false
	}
	return quantityFromMap(usage, name, true)
}

// usageQuantityValue 读取指标对象的 usage.<name>（内存用字节）。
func usageQuantityValue(item *unstructured.Unstructured, name string) (int64, bool) {
	usage, found, err := unstructured.NestedMap(item.Object, "usage")
	if err != nil || !found {
		return 0, false
	}
	return quantityFromMap(usage, name, false)
}

func quantityFromMap(values map[string]any, name string, milli bool) (int64, bool) {
	raw, ok := values[name].(string)
	if !ok || raw == "" {
		return 0, false
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0, false
	}
	if milli {
		return quantity.MilliValue(), true
	}
	return quantity.Value(), true
}

// metricTimestamp 使用指标自带的采样时间；缺失时退回本次采集时间。
func metricTimestamp(items []unstructured.Unstructured, fallback time.Time) time.Time {
	if len(items) == 0 {
		return fallback
	}
	raw, found, err := unstructured.NestedString(items[0].Object, "timestamp")
	if err != nil || !found || raw == "" {
		return fallback
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return fallback
	}
	return parsed
}

// listPerNamespace 并发读取给定命名空间的指标，返回汇总结果与读取成功的命名空间数。
// 单个命名空间失败（含无权）直接跳过，不泄露其存在性。
func listPerNamespace(
	ctx context.Context,
	t Transport,
	gvr schema.GroupVersionResource,
	namespaces []string,
	sem chan struct{},
) ([]unstructured.Unstructured, int) {
	if len(namespaces) == 0 {
		return []unstructured.Unstructured{}, 0
	}
	results := make([]struct {
		items []unstructured.Unstructured
		err   error
	}, len(namespaces))
	var wg sync.WaitGroup
	for i, namespace := range namespaces {
		wg.Add(1)
		go func(index int, ns string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items, _, err := listAll(ctx, t, gvr, ns)
			results[index] = struct {
				items []unstructured.Unstructured
				err   error
			}{items, err}
		}(i, namespace)
	}
	wg.Wait()

	collected := make([]unstructured.Unstructured, 0)
	succeed := 0
	for _, result := range results {
		if result.err != nil {
			continue
		}
		succeed++
		collected = append(collected, result.items...)
		if len(collected) >= overviewMaxObjects {
			collected = collected[:overviewMaxObjects]
			break
		}
	}
	return collected, succeed
}
