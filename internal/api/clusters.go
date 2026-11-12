package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/kube"
)

// transport 从路径参数解析集群；不存在时返回统一 404。
func (h *handlers) transport(w http.ResponseWriter, r *http.Request) (kube.Transport, bool) {
	if h.registry == nil {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return nil, false
	}
	t, ok := h.registry.Get(chi.URLParam(r, "cluster"))
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return nil, false
	}
	return t, true
}

// listClusters 返回全部已注册集群及其状态。
func (h *handlers) listClusters(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		writeJSON(w, http.StatusOK, []kube.ClusterInfo{})
		return
	}
	transports := h.registry.All()
	out := make([]kube.ClusterInfo, 0, len(transports))
	for _, t := range transports {
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
