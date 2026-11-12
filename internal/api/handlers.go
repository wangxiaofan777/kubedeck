package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/kubedeck/kubedeck/internal/kube"
)

type handlers struct {
	registry *kube.Registry
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// transport 从路径参数解析集群；不存在时返回统一 404。
func (h *handlers) transport(w http.ResponseWriter, r *http.Request) (kube.Transport, bool) {
	t, ok := h.registry.Get(chi.URLParam(r, "cluster"))
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return nil, false
	}
	return t, true
}

// listClusters 返回全部已注册集群及其状态。
func (h *handlers) listClusters(w http.ResponseWriter, r *http.Request) {
	out := make([]kube.ClusterInfo, 0, len(h.registry.All()))
	for _, t := range h.registry.All() {
		out = append(out, t.Info(r.Context()))
	}
	writeJSON(w, http.StatusOK, out)
}

// listNamespaces 返回集群的命名空间列表。
func (h *handlers) listNamespaces(w http.ResponseWriter, r *http.Request) {
	t, ok := h.transport(w, r)
	if !ok {
		return
	}
	names, err := t.Namespaces(r.Context())
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": names})
}

// listResponse 是资源列表响应。
type listResponse struct {
	Items     []map[string]any `json:"items"`
	Continue  string           `json:"continue,omitempty"`
	Remaining *int64           `json:"remainingItemCount,omitempty"`
}

// listResources 列出任意 GVR 的资源。
func (h *handlers) listResources(w http.ResponseWriter, r *http.Request) {
	t, ok := h.transport(w, r)
	if !ok {
		return
	}

	gvr, ok := gvrFromPath(r)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "group/version/resource 不能为空"))
		return
	}

	q := r.URL.Query()
	limit := int64(200)
	if v := q.Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 || n > 500 {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "limit 必须在 1-500 之间"))
			return
		}
		limit = n
	}

	list, err := t.List(r.Context(), gvr, q.Get("namespace"), kube.ListOptions{
		Limit:         limit,
		Continue:      q.Get("continue"),
		LabelSelector: q.Get("labelSelector"),
	})
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	items := make([]map[string]any, 0, len(list.Items))
	for i := range list.Items {
		items = append(items, list.Items[i].Object)
	}
	writeJSON(w, http.StatusOK, listResponse{
		Items:     items,
		Continue:  list.GetContinue(),
		Remaining: list.GetRemainingItemCount(),
	})
}

// getResource 读取单个资源；?format=yaml 返回 YAML 文本。
func (h *handlers) getResource(w http.ResponseWriter, r *http.Request) {
	t, ok := h.transport(w, r)
	if !ok {
		return
	}

	gvr, ok := gvrFromPath(r)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "group/version/resource 不能为空"))
		return
	}

	obj, err := t.Get(r.Context(), gvr, r.URL.Query().Get("namespace"), chi.URLParam(r, "name"))
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}

	if r.URL.Query().Get("format") == "yaml" {
		// managedFields 对用户无意义，展示前移除。
		if meta, ok := obj.Object["metadata"].(map[string]any); ok {
			delete(meta, "managedFields")
		}
		b, err := yaml.Marshal(obj.Object)
		if err != nil {
			WriteError(r.Context(), w, NewError(CodeInternal, "YAML 渲染失败"))
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
		return
	}

	writeJSON(w, http.StatusOK, obj.Object)
}

// gvrFromPath 解析路径中的 {group}/{version}/{resource}；group 用 "_" 表示 core 组。
func gvrFromPath(r *http.Request) (schema.GroupVersionResource, bool) {
	group := chi.URLParam(r, "group")
	version := chi.URLParam(r, "version")
	resource := chi.URLParam(r, "resource")
	if group == "_" {
		group = ""
	}
	if version == "" || resource == "" {
		return schema.GroupVersionResource{}, false
	}
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}, true
}
