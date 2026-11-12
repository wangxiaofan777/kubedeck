package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
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
	t, cluster, err := h.transportFor(r.Context(), id, chi.URLParam(r, "cluster"), r.URL.Query().Get("namespace"))
	if err != nil {
		status, message := transportError(err)
		WriteError(r.Context(), w, NewError(status, message))
		return nil, nil, false
	}
	return t, cluster, true
}

// transportFor 是可复用的集群访问入口（操作进度查询等场景使用）。
func (h *handlers) transportFor(ctx context.Context, id auth.Identity, name, namespace string) (kube.Transport, kube.ClusterAccess, error) {
	allowed, err := h.clusterAccess(ctx, id, name, namespace)
	if err != nil {
		return nil, nil, err
	}
	if !allowed {
		return nil, nil, errForbidden
	}
	cluster, ok := h.cluster(name)
	if !ok {
		return nil, nil, domain.ErrNotFound
	}
	if info := cluster.Info(ctx); info.Status == kube.StatusUnreachable {
		return nil, nil, errClusterUnreachable
	}
	t, err := cluster.TransportFor(id.UserRef())
	if err != nil {
		return nil, nil, fmt.Errorf("build transport: %w", err)
	}
	return t, cluster, nil
}

var (
	errForbidden          = errors.New("api: forbidden")
	errClusterUnreachable = errors.New("api: cluster unreachable")
)

func transportError(err error) (ErrorCode, string) {
	switch {
	case errors.Is(err, errForbidden):
		return CodeForbidden, "无权访问该集群或命名空间"
	case errors.Is(err, domain.ErrNotFound):
		return CodeNotFound, "集群不存在"
	case errors.Is(err, errClusterUnreachable):
		return CodeClusterUnreachable, "目标集群不可达，请稍后重试"
	case errors.Is(err, domain.ErrConflict):
		return CodeConflict, "资源状态冲突"
	default:
		return CodeInternal, "无法建立集群连接"
	}
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
