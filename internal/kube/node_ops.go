package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var (
	nodeGVR = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
	podGVR  = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
)

// DrainOptions 控制节点驱逐的安全边界。
type DrainOptions struct {
	Cordon             bool
	Force              bool
	IgnoreDaemonSets   bool
	DeleteEmptyDirData bool
	GracePeriodSeconds *int64
	Timeout            time.Duration
}

// DrainResult 是单个 Pod 的驱逐结果。Result 取值：evicted / skipped / blocked / failed。
type DrainResult struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	Result    string `json:"result"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

// DrainReport 汇总一次 drain 的结果。
type DrainReport struct {
	Node     string        `json:"node"`
	Cordoned bool          `json:"cordoned"`
	Evicted  int           `json:"evicted"`
	Skipped  int           `json:"skipped"`
	Blocked  int           `json:"blocked"`
	Failed   int           `json:"failed"`
	Results  []DrainResult `json:"results"`
}

const (
	drainPageSize  = 500
	drainMaxPages  = 20
	drainErrLength = 200
)

// SetNodeSchedulable 通过 merge patch 修改节点是否可调度。
func SetNodeSchedulable(ctx context.Context, t Transport, name string, schedulable bool) (*unstructured.Unstructured, error) {
	patch := fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, !schedulable)
	return t.Patch(ctx, nodeGVR, "", name, types.MergePatchType, []byte(patch), PatchOptions{})
}

// DrainNode 驱逐节点上的 Pod：默认跳过 DaemonSet 与镜像 Pod，
// 未受控 Pod 与使用 emptyDir 的 Pod 默认阻止，需要显式放开。
func DrainNode(ctx context.Context, t Transport, name string, opts DrainOptions) (DrainReport, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	report := DrainReport{Node: name, Results: make([]DrainResult, 0, 16)}
	if _, err := t.Get(ctx, nodeGVR, "", name); err != nil {
		return report, err
	}
	if opts.Cordon {
		if _, err := SetNodeSchedulable(ctx, t, name, false); err != nil {
			return report, err
		}
		report.Cordoned = true
	}

	pods, err := listPodsOnNode(ctx, t, name)
	if err != nil {
		return report, err
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].GetNamespace() != pods[j].GetNamespace() {
			return pods[i].GetNamespace() < pods[j].GetNamespace()
		}
		return pods[i].GetName() < pods[j].GetName()
	})

	for i := range pods {
		if ctx.Err() != nil {
			report.Failed++
			report.Results = append(report.Results, DrainResult{
				Namespace: pods[i].GetNamespace(), Name: pods[i].GetName(),
				Result: "failed", Code: "TIMEOUT", Message: "整体超时，未处理该 Pod",
			})
			continue
		}
		result := classifyPodForDrain(&pods[i], opts)
		if result.Result != "evict" {
			report.Results = append(report.Results, result)
			if result.Result == "skipped" {
				report.Skipped++
			} else {
				report.Blocked++
			}
			continue
		}
		ns, podName := pods[i].GetNamespace(), pods[i].GetName()
		if err := t.Evict(ctx, ns, podName, opts.GracePeriodSeconds); err != nil {
			code, message := drainError(err)
			report.Failed++
			report.Results = append(report.Results, DrainResult{
				Namespace: ns, Name: podName, Result: "blocked", Code: code, Message: message,
			})
			continue
		}
		report.Evicted++
		report.Results = append(report.Results, DrainResult{
			Namespace: ns, Name: podName, Result: "evicted",
		})
	}
	return report, nil
}

func listPodsOnNode(ctx context.Context, t Transport, node string) ([]unstructured.Unstructured, error) {
	opts := ListOptions{
		Limit:         drainPageSize,
		FieldSelector: "spec.nodeName=" + node,
	}
	out := make([]unstructured.Unstructured, 0, drainPageSize)
	for page := 0; ; page++ {
		if page >= drainMaxPages {
			return nil, fmt.Errorf("pods on node exceeded %d pages", drainMaxPages)
		}
		list, err := t.List(ctx, podGVR, "", opts)
		if err != nil {
			return nil, err
		}
		out = append(out, list.Items...)
		next := list.GetContinue()
		if next == "" {
			return out, nil
		}
		opts.Continue = next
	}
}

// classifyPodForDrain 返回 evict 或带原因的 skipped/blocked。
func classifyPodForDrain(pod *unstructured.Unstructured, opts DrainOptions) DrainResult {
	result := DrainResult{Namespace: pod.GetNamespace(), Name: pod.GetName()}

	phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	if phase == "Succeeded" || phase == "Failed" {
		result.Result, result.Code, result.Message = "skipped", "TERMINATED", "Pod 已终止"
		return result
	}
	if _, ok := pod.GetAnnotations()["kubernetes.io/config.mirror"]; ok {
		result.Result, result.Code, result.Message = "skipped", "MIRROR_POD", "镜像 Pod 由 kubelet 管理"
		return result
	}
	if owner := controllerOwnerKind(pod); owner == "DaemonSet" && opts.IgnoreDaemonSets {
		result.Result, result.Code, result.Message = "skipped", "DAEMONSET", "DaemonSet 管理的 Pod"
		return result
	}
	if controllerOwnerKind(pod) == "" && !opts.Force {
		result.Result, result.Code, result.Message = "blocked", "UNMANAGED", "Pod 没有控制器 Owner，需显式强制"
		return result
	}
	if hasEmptyDir(pod) && !opts.DeleteEmptyDirData {
		result.Result, result.Code, result.Message = "blocked", "LOCAL_STORAGE", "Pod 使用 emptyDir，需显式允许删除本地数据"
		return result
	}
	result.Result = "evict"
	return result
}

func controllerOwnerKind(pod *unstructured.Unstructured) string {
	refs := pod.GetOwnerReferences()
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return refs[i].Kind
		}
	}
	return ""
}

func hasEmptyDir(pod *unstructured.Unstructured) bool {
	volumes, found, _ := unstructured.NestedSlice(pod.Object, "spec", "volumes")
	if !found {
		return false
	}
	for _, v := range volumes {
		volume, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := volume["emptyDir"]; exists {
			return true
		}
	}
	return false
}

func drainError(err error) (string, string) {
	msg := err.Error()
	if len(msg) > drainErrLength {
		msg = msg[:drainErrLength]
	}
	switch {
	case apierrors.IsTooManyRequests(err):
		return "PDB", msg
	case apierrors.IsForbidden(err):
		if strings.Contains(msg, "disruption budget") || strings.Contains(msg, "PodDisruptionBudget") {
			return "PDB", msg
		}
		return "FORBIDDEN", "没有驱逐该 Pod 的权限"
	case apierrors.IsNotFound(err):
		return "NOT_FOUND", "Pod 已不存在"
	default:
		return "OTHER", msg
	}
}

// NodePods 返回节点上的全部 Pod，按命名空间与名称排序。
func NodePods(ctx context.Context, t Transport, node string) ([]unstructured.Unstructured, error) {
	pods, err := listPodsOnNode(ctx, t, node)
	if err != nil {
		return nil, err
	}
	sort.Slice(pods, func(i, j int) bool {
		if pods[i].GetNamespace() != pods[j].GetNamespace() {
			return pods[i].GetNamespace() < pods[j].GetNamespace()
		}
		return pods[i].GetName() < pods[j].GetName()
	})
	return pods, nil
}
