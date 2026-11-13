package kube

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ErrOverviewDegraded 表示集群可连接，但没有任何统计项读取成功。
var ErrOverviewDegraded = errors.New("kube: overview degraded")

const (
	overviewPageSize          = 500
	overviewMaxObjects        = 5000
	overviewEventLimit        = 20
	overviewEventMessageRunes = 200
	overviewSectionTimeout    = 5 * time.Second
	overviewCollectTimeout    = 8 * time.Second
	overviewParallelism       = 8
)

var (
	overviewNodesGVR        = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
	overviewPodsGVR         = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	overviewEventsGVR       = schema.GroupVersionResource{Version: "v1", Resource: "events"}
	overviewDeploymentsGVR  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	overviewStatefulSetsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	overviewDaemonSetsGVR   = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
)

// Overview 是单集群总览的聚合结果。所有计数均按当前用户身份实时读取。
type Overview struct {
	Cluster     string            `json:"cluster"`
	FetchedAt   time.Time         `json:"fetchedAt"`
	Scope       OverviewScope     `json:"scope"`
	Nodes       OverviewNodes     `json:"nodes"`
	Namespaces  OverviewNamespace `json:"namespaces"`
	Pods        OverviewPods      `json:"pods"`
	Workloads   OverviewWorkloads `json:"workloads"`
	Events      []OverviewEvent   `json:"events"`
	Unavailable []string          `json:"unavailable"`
}

// OverviewScope 描述统计覆盖范围。
type OverviewScope struct {
	AllNamespaces bool `json:"allNamespaces"`
	Namespaces    int  `json:"namespaces"`
	Truncated     bool `json:"truncated"`
}

// OverviewAllocatable 是节点可分配资源的汇总值。
type OverviewAllocatable struct {
	CPUMilli    int64 `json:"cpuMilli"`
	MemoryBytes int64 `json:"memoryBytes"`
}

// OverviewNodes 是节点统计。
type OverviewNodes struct {
	Total       int                 `json:"total"`
	Ready       int                 `json:"ready"`
	Allocatable OverviewAllocatable `json:"allocatable"`
}

// OverviewNamespace 是命名空间统计。
type OverviewNamespace struct {
	Total int `json:"total"`
}

// OverviewPods 是 Pod 状态分布；CrashLoop 与 phase 分类互不覆盖。
type OverviewPods struct {
	Total     int `json:"total"`
	Running   int `json:"running"`
	Pending   int `json:"pending"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Unknown   int `json:"unknown"`
	CrashLoop int `json:"crashLoop"`
}

// WorkloadCount 是单类工作负载的总数与异常数。
type WorkloadCount struct {
	Total       int `json:"total"`
	Unavailable int `json:"unavailable"`
}

// OverviewWorkloads 是工作负载统计。
type OverviewWorkloads struct {
	Deployments  WorkloadCount `json:"deployments"`
	StatefulSets WorkloadCount `json:"statefulSets"`
	DaemonSets   WorkloadCount `json:"daemonSets"`
}

// OverviewEvent 是最近事件摘要。
type OverviewEvent struct {
	Type          string    `json:"type"`
	Reason        string    `json:"reason"`
	Namespace     string    `json:"namespace,omitempty"`
	Object        string    `json:"object"`
	Message       string    `json:"message"`
	LastTimestamp time.Time `json:"lastTimestamp"`
}

// OverviewOptions 限定总览的统计范围。
// NamespacesKnown 为 true 时直接使用 Namespaces，不再向 apiserver 枚举；
// 平台把用户的命名空间绑定传给总览，避免要求用户额外具备 namespace 列表权限。
type OverviewOptions struct {
	Namespaces      []string
	NamespacesKnown bool
}

// CollectOverview 以当前用户身份汇总单集群总览。
// 单项失败只影响该项并记入 Unavailable；所有项都失败时返回 ErrOverviewDegraded。
func CollectOverview(ctx context.Context, cluster string, t Transport, opts OverviewOptions) (Overview, error) {
	overview := Overview{
		Cluster:     cluster,
		FetchedAt:   time.Now().UTC(),
		Events:      []OverviewEvent{},
		Unavailable: []string{},
	}
	overview.Scope.AllNamespaces = true

	collectCtx, cancel := context.WithTimeout(ctx, overviewCollectTimeout)
	defer cancel()

	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		succeeded int
		fellBack  bool
	)
	markUnavailable := func(name string) {
		mu.Lock()
		overview.Unavailable = append(overview.Unavailable, name)
		mu.Unlock()
	}
	markSucceeded := func() {
		mu.Lock()
		succeeded++
		mu.Unlock()
	}
	markTruncated := func() {
		mu.Lock()
		overview.Scope.Truncated = true
		mu.Unlock()
	}
	markFellBack := func() {
		mu.Lock()
		fellBack = true
		mu.Unlock()
	}

	// 命名空间既是统计项，也是回退列表；先取一次，失败不阻断其它项。
	namespaces, namespacesKnown := collectNamespaces(collectCtx, t, opts, func(total int) {
		mu.Lock()
		overview.Namespaces.Total = total
		mu.Unlock()
	}, markUnavailable, markSucceeded)

	// 所有回退读取共享同一个并发闸门，避免多类资源同时回退时放大请求。
	sem := make(chan struct{}, overviewParallelism)

	// 节点：集群级资源，没有回退路径。
	wg.Add(1)
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(collectCtx, overviewSectionTimeout)
		defer cancel()
		nodes, err := collectNodes(sectionCtx, t)
		if err != nil {
			markUnavailable("nodes")
			return
		}
		mu.Lock()
		overview.Nodes = nodes
		mu.Unlock()
		markSucceeded()
	}()

	// Pod：先尝试全命名空间，403 时按可见命名空间回退。
	wg.Add(1)
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(collectCtx, overviewSectionTimeout)
		defer cancel()
		items, allNamespaces, truncated, err := listScoped(sectionCtx, t, overviewPodsGVR, namespaces, namespacesKnown, sem)
		if err != nil {
			markUnavailable("pods")
			return
		}
		if truncated {
			markTruncated()
		}
		if !allNamespaces {
			markFellBack()
		}
		pods := summarizePods(items)
		mu.Lock()
		overview.Pods = pods
		mu.Unlock()
		markSucceeded()
	}()

	// 工作负载：三类资源分别统计，单项失败只标记单项。
	wg.Add(1)
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(collectCtx, overviewSectionTimeout)
		defer cancel()
		kinds := []struct {
			name      string
			gvr       schema.GroupVersionResource
			summarize func([]unstructured.Unstructured) WorkloadCount
			assign    func(*OverviewWorkloads, WorkloadCount)
		}{
			{"deployments", overviewDeploymentsGVR, summarizeDeployment, func(w *OverviewWorkloads, c WorkloadCount) { w.Deployments = c }},
			{"statefulsets", overviewStatefulSetsGVR, summarizeStatefulSet, func(w *OverviewWorkloads, c WorkloadCount) { w.StatefulSets = c }},
			{"daemonsets", overviewDaemonSetsGVR, summarizeDaemonSet, func(w *OverviewWorkloads, c WorkloadCount) { w.DaemonSets = c }},
		}
		for _, kind := range kinds {
			items, allNamespaces, truncated, err := listScoped(sectionCtx, t, kind.gvr, namespaces, namespacesKnown, sem)
			if err != nil {
				markUnavailable(kind.name)
				continue
			}
			if truncated {
				markTruncated()
			}
			if !allNamespaces {
				markFellBack()
			}
			count := kind.summarize(items)
			mu.Lock()
			kind.assign(&overview.Workloads, count)
			mu.Unlock()
			markSucceeded()
		}
	}()

	// 事件：只取最近 N 条，读取失败只影响事件区域。
	wg.Add(1)
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(collectCtx, overviewSectionTimeout)
		defer cancel()
		items, allNamespaces, truncated, err := listScoped(sectionCtx, t, overviewEventsGVR, namespaces, namespacesKnown, sem)
		if err != nil {
			markUnavailable("events")
			return
		}
		if truncated {
			markTruncated()
		}
		if !allNamespaces {
			markFellBack()
		}
		events := summarizeEvents(items)
		mu.Lock()
		overview.Events = events
		mu.Unlock()
		markSucceeded()
	}()

	wg.Wait()

	overview.Scope.Namespaces = len(namespaces)
	overview.Scope.AllNamespaces = !fellBack
	sort.Strings(overview.Unavailable)
	if succeeded == 0 {
		return Overview{}, ErrOverviewDegraded
	}
	return overview, nil
}

// collectNamespaces 读取用户可见命名空间；失败时返回 nil 与 false。
func collectNamespaces(
	ctx context.Context,
	t Transport,
	opts OverviewOptions,
	onTotal func(int),
	onUnavailable func(string),
	onSucceeded func(),
) ([]string, bool) {
	if opts.NamespacesKnown {
		onTotal(len(opts.Namespaces))
		onSucceeded()
		return opts.Namespaces, true
	}
	sectionCtx, cancel := context.WithTimeout(ctx, overviewSectionTimeout)
	defer cancel()
	namespaces, err := t.Namespaces(sectionCtx)
	if err != nil {
		onUnavailable("namespaces")
		return nil, false
	}
	onTotal(len(namespaces))
	onSucceeded()
	return namespaces, true
}

func collectNodes(ctx context.Context, t Transport) (OverviewNodes, error) {
	items, _, err := listAll(ctx, t, overviewNodesGVR, "")
	if err != nil {
		return OverviewNodes{}, err
	}
	var out OverviewNodes
	for i := range items {
		node := &items[i]
		out.Total++
		if nodeReady(node) {
			out.Ready++
		}
		// 单个节点的容量解析失败只跳过该值，不影响节点计数。
		if cpu, ok := quantityMilli(node, "cpu"); ok {
			out.Allocatable.CPUMilli += cpu
		}
		if memory, ok := quantityValue(node, "memory"); ok {
			out.Allocatable.MemoryBytes += memory
		}
	}
	return out, nil
}

func nodeReady(node *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(node.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, condition := range conditions {
		entry, ok := condition.(map[string]any)
		if !ok {
			continue
		}
		if entry["type"] != "Ready" {
			continue
		}
		return entry["status"] == "True"
	}
	return false
}

func quantityMilli(node *unstructured.Unstructured, name string) (int64, bool) {
	raw, found, err := unstructured.NestedString(node.Object, "status", "allocatable", name)
	if err != nil || !found || raw == "" {
		return 0, false
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0, false
	}
	return quantity.MilliValue(), true
}

func quantityValue(node *unstructured.Unstructured, name string) (int64, bool) {
	raw, found, err := unstructured.NestedString(node.Object, "status", "allocatable", name)
	if err != nil || !found || raw == "" {
		return 0, false
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0, false
	}
	return quantity.Value(), true
}

func summarizePods(items []unstructured.Unstructured) OverviewPods {
	var out OverviewPods
	for i := range items {
		pod := &items[i]
		out.Total++
		phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
		switch phase {
		case "Running":
			out.Running++
		case "Pending":
			out.Pending++
		case "Succeeded":
			out.Succeeded++
		case "Failed":
			out.Failed++
		default:
			out.Unknown++
		}
		if podCrashLooping(pod) {
			out.CrashLoop++
		}
	}
	return out
}

func podCrashLooping(pod *unstructured.Unstructured) bool {
	for _, field := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		statuses, found, err := unstructured.NestedSlice(pod.Object, "status", field)
		if err != nil || !found {
			continue
		}
		for _, status := range statuses {
			entry, ok := status.(map[string]any)
			if !ok {
				continue
			}
			state, ok := entry["state"].(map[string]any)
			if !ok {
				continue
			}
			waiting, ok := state["waiting"].(map[string]any)
			if !ok {
				continue
			}
			reason, _ := waiting["reason"].(string)
			if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" {
				return true
			}
		}
	}
	return false
}

func summarizeDeployment(items []unstructured.Unstructured) WorkloadCount {
	var out WorkloadCount
	for i := range items {
		item := &items[i]
		out.Total++
		desired := int64(1)
		if value, found, err := unstructured.NestedInt64(item.Object, "spec", "replicas"); err == nil && found {
			desired = value
		}
		ready, _, _ := unstructured.NestedInt64(item.Object, "status", "readyReplicas")
		if ready < desired {
			out.Unavailable++
		}
	}
	return out
}

func summarizeStatefulSet(items []unstructured.Unstructured) WorkloadCount {
	// StatefulSet 与 Deployment 的副本口径一致。
	return summarizeDeployment(items)
}

func summarizeDaemonSet(items []unstructured.Unstructured) WorkloadCount {
	var out WorkloadCount
	for i := range items {
		item := &items[i]
		out.Total++
		desired, _, _ := unstructured.NestedInt64(item.Object, "status", "desiredNumberScheduled")
		ready, _, _ := unstructured.NestedInt64(item.Object, "status", "numberReady")
		if ready < desired {
			out.Unavailable++
		}
	}
	return out
}

func summarizeEvents(items []unstructured.Unstructured) []OverviewEvent {
	events := make([]OverviewEvent, 0, len(items))
	for i := range items {
		item := &items[i]
		eventType, _, _ := unstructured.NestedString(item.Object, "type")
		reason, _, _ := unstructured.NestedString(item.Object, "reason")
		message, _, _ := unstructured.NestedString(item.Object, "message")
		namespace, _, _ := unstructured.NestedString(item.Object, "metadata", "namespace")
		kind, _, _ := unstructured.NestedString(item.Object, "involvedObject", "kind")
		name, _, _ := unstructured.NestedString(item.Object, "involvedObject", "name")
		object := name
		if kind != "" && name != "" {
			object = kind + "/" + name
		}
		events = append(events, OverviewEvent{
			Type:          eventType,
			Reason:        reason,
			Namespace:     namespace,
			Object:        object,
			Message:       truncateRunes(message, overviewEventMessageRunes),
			LastTimestamp: eventTimestamp(item),
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].LastTimestamp.After(events[j].LastTimestamp)
	})
	if len(events) > overviewEventLimit {
		events = events[:overviewEventLimit]
	}
	return events
}

func eventTimestamp(item *unstructured.Unstructured) time.Time {
	paths := [][]string{
		{"lastTimestamp"},
		{"eventTime"},
		{"metadata", "creationTimestamp"},
	}
	for _, path := range paths {
		raw, found, err := unstructured.NestedString(item.Object, path...)
		if err != nil || !found || raw == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

// listScoped 先尝试全命名空间读取；被拒绝时按可见命名空间逐个读取。
// allNamespaces 为 false 表示走了回退路径。单个命名空间失败（含无权）直接跳过。
func listScoped(
	ctx context.Context,
	t Transport,
	gvr schema.GroupVersionResource,
	namespaces []string,
	namespacesKnown bool,
	sem chan struct{},
) (items []unstructured.Unstructured, allNamespaces bool, truncated bool, err error) {
	items, truncated, err = listAll(ctx, t, gvr, "")
	if err == nil {
		return items, true, truncated, nil
	}
	if !apierrors.IsForbidden(err) || !namespacesKnown {
		return nil, false, false, err
	}
	if len(namespaces) == 0 {
		return []unstructured.Unstructured{}, false, false, nil
	}

	results := make([]struct {
		items     []unstructured.Unstructured
		truncated bool
		err       error
	}, len(namespaces))
	var wg sync.WaitGroup
	for i, namespace := range namespaces {
		wg.Add(1)
		go func(index int, ns string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			listed, wasTruncated, listErr := listAll(ctx, t, gvr, ns)
			results[index] = struct {
				items     []unstructured.Unstructured
				truncated bool
				err       error
			}{listed, wasTruncated, listErr}
		}(i, namespace)
	}
	wg.Wait()

	collected := make([]unstructured.Unstructured, 0)
	for _, result := range results {
		// 无权限或单命名空间失败的命名空间直接跳过，既不报错也不泄露其存在性。
		if result.err != nil {
			continue
		}
		if result.truncated {
			truncated = true
		}
		collected = append(collected, result.items...)
		if len(collected) >= overviewMaxObjects {
			collected = collected[:overviewMaxObjects]
			truncated = true
			break
		}
	}
	return collected, false, truncated, nil
}

// listAll 按分页读取一类资源，最多返回 overviewMaxObjects 个对象。
func listAll(
	ctx context.Context,
	t Transport,
	gvr schema.GroupVersionResource,
	namespace string,
) ([]unstructured.Unstructured, bool, error) {
	out := make([]unstructured.Unstructured, 0)
	token := ""
	for {
		list, err := t.List(ctx, gvr, namespace, ListOptions{Limit: overviewPageSize, Continue: token})
		if err != nil {
			return nil, false, err
		}
		if list == nil {
			return out, false, nil
		}
		out = append(out, list.Items...)
		if len(out) >= overviewMaxObjects {
			return out[:overviewMaxObjects], true, nil
		}
		token = list.GetContinue()
		if token == "" {
			return out, false, nil
		}
	}
}
