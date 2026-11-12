package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"sigs.k8s.io/yaml"

	"github.com/kubedeck/kubedeck/internal/kube"
)

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
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}

	list, err := t.List(r.Context(), gvr, r.URL.Query().Get("namespace"), kube.ListOptions{
		Limit:         limit,
		Continue:      r.URL.Query().Get("continue"),
		LabelSelector: r.URL.Query().Get("labelSelector"),
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
		body, err := yaml.Marshal(obj.Object)
		if err != nil {
			WriteError(r.Context(), w, NewError(CodeInternal, "YAML 渲染失败"))
			return
		}
		writeYAML(w, body)
		return
	}

	writeJSON(w, http.StatusOK, obj.Object)
}
