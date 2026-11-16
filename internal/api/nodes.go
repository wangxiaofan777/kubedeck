package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/kube"
)

const maxDrainTimeout = 300 // 秒

// nodeAction 是节点调度操作的公共实现。
func (h *handlers) nodeAction(w http.ResponseWriter, r *http.Request, schedulable bool) {
	t, _, ok := h.transport(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	node, err := kube.SetNodeSchedulable(r.Context(), t, name, schedulable)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	unschedulable, _, _ := nestedBool(node.Object, "spec", "unschedulable")
	writeJSON(w, http.StatusOK, map[string]any{
		"name":            node.GetName(),
		"schedulable":     !unschedulable,
		"resourceVersion": node.GetResourceVersion(),
	})
}

func (h *handlers) cordonNode(w http.ResponseWriter, r *http.Request) {
	h.nodeAction(w, r, false)
}

func (h *handlers) uncordonNode(w http.ResponseWriter, r *http.Request) {
	h.nodeAction(w, r, true)
}

// nodePods 返回节点上的 Pod 摘要。
func (h *handlers) nodePods(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.transport(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	pods, err := kube.NodePods(r.Context(), t, name)
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	items := make([]map[string]any, 0, len(pods))
	for i := range pods {
		items = append(items, summarize(&pods[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": name, "items": items})
}

type drainRequest struct {
	Cordon             *bool  `json:"cordon"`
	Force              bool   `json:"force"`
	IgnoreDaemonSets   *bool  `json:"ignoreDaemonSets"`
	DeleteEmptyDirData bool   `json:"deleteEmptyDirData"`
	GracePeriodSeconds *int64 `json:"gracePeriodSeconds"`
	TimeoutSeconds     int    `json:"timeoutSeconds"`
}

// drainNode 驱逐节点上的 Pod。逐项结果即使部分失败也返回 200；
// 全部失败且均由 PDB 阻止时返回 409，便于前端突出显示。
func (h *handlers) drainNode(w http.ResponseWriter, r *http.Request) {
	t, _, ok := h.transport(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	if !validObjectName(w, r.Context(), name) {
		return
	}
	var body drainRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cordon := true
	if body.Cordon != nil {
		cordon = *body.Cordon
	}
	ignoreDaemonSets := true
	if body.IgnoreDaemonSets != nil {
		ignoreDaemonSets = *body.IgnoreDaemonSets
	}
	timeout := 120 * time.Second
	if body.TimeoutSeconds > 0 {
		if body.TimeoutSeconds > maxDrainTimeout {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "timeoutSeconds 最大 300"))
			return
		}
		timeout = time.Duration(body.TimeoutSeconds) * time.Second
	}

	report, err := kube.DrainNode(r.Context(), t, name, kube.DrainOptions{
		Cordon:             cordon,
		Force:              body.Force,
		IgnoreDaemonSets:   ignoreDaemonSets,
		DeleteEmptyDirData: body.DeleteEmptyDirData,
		GracePeriodSeconds: body.GracePeriodSeconds,
		Timeout:            timeout,
	})
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	pdbBlocked := 0
	for _, item := range report.Results {
		if item.Code == "PDB" {
			pdbBlocked++
		}
	}
	if report.Evicted == 0 && report.Skipped == 0 && report.Blocked == 0 &&
		report.Failed > 0 && pdbBlocked == report.Failed {
		e := NewError(CodeConflict, "所有 Pod 均被 PodDisruptionBudget 阻止，节点未完成驱逐")
		e.Details = map[string]any{"drain": report}
		WriteError(r.Context(), w, e)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// nestedBool 读取嵌套布尔字段，缺失时返回 false。
func nestedBool(obj map[string]any, fields ...string) (bool, bool, error) {
	var cur any = obj
	for _, field := range fields {
		next, ok := cur.(map[string]any)
		if !ok {
			return false, false, nil
		}
		cur = next[field]
	}
	value, ok := cur.(bool)
	return value, ok, nil
}
