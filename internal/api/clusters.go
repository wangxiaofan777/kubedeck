package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

// transport 解析集群与当前用户身份，并执行平台准入检查。
// 未授权时返回不含集群名称的 403（规格：集群可见范围）。
func (h *handlers) transport(w http.ResponseWriter, r *http.Request) (kube.Transport, kube.ClusterAccess, bool) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return nil, nil, false
	}
	name := chi.URLParam(r, "cluster")
	namespace := r.URL.Query().Get("namespace")

	allowed, err := h.clusterAccess(r.Context(), id, name, namespace)
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return nil, nil, false
	}
	if !allowed {
		WriteError(r.Context(), w, NewError(CodeForbidden, "无权访问该集群或命名空间"))
		return nil, nil, false
	}
	cluster, ok := h.cluster(name)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return nil, nil, false
	}
	if info := cluster.Info(r.Context()); info.Status == kube.StatusUnreachable {
		WriteError(r.Context(), w, NewError(CodeClusterUnreachable, "目标集群不可达，请稍后重试"))
		return nil, nil, false
	}
	t, err := cluster.TransportFor(id.UserRef())
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "无法建立集群连接"))
		return nil, nil, false
	}
	return t, cluster, true
}

// listClusters 返回当前用户可见的集群及其状态。
func (h *handlers) listClusters(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	if h.registry == nil {
		writeJSON(w, http.StatusOK, []kube.ClusterInfo{})
		return
	}

	clusters := h.registry.All()
	if !id.PlatformAdmin {
		bindings, err := h.bindingsFor(r.Context(), id)
		if err != nil {
			WriteError(r.Context(), w, FromPlatformError(err))
			return
		}
		visible := make(map[string]struct{}, len(bindings))
		for _, b := range bindings {
			visible[b.ClusterName] = struct{}{}
		}
		filtered := clusters[:0]
		for _, c := range clusters {
			if _, ok := visible[c.Name()]; ok {
				filtered = append(filtered, c)
			}
		}
		clusters = filtered
	}

	out := make([]kube.ClusterInfo, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c.Info(r.Context()))
	}
	writeJSON(w, http.StatusOK, out)
}

// listNamespaces 返回当前用户可访问的命名空间。
func (h *handlers) listNamespaces(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	t, cluster, ok := h.transport(w, r)
	if !ok {
		return
	}

	if !id.PlatformAdmin {
		bindings, err := h.bindingsFor(r.Context(), id)
		if err != nil {
			WriteError(r.Context(), w, FromPlatformError(err))
			return
		}
		names := make([]string, 0)
		all := false
		for _, b := range bindings {
			if b.ClusterName != cluster.Name() {
				continue
			}
			if b.Namespace == "*" {
				all = true
				break
			}
			names = append(names, b.Namespace)
		}
		if !all {
			writeJSON(w, http.StatusOK, map[string]any{"namespaces": names})
			return
		}
	}

	names, err := t.Namespaces(r.Context())
	if err != nil {
		WriteError(r.Context(), w, FromUpstream(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"namespaces": names})
}
