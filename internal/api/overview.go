package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

// clusterOverview 返回单集群只读总览。统计按当前用户身份实时读取，不落库。
func (h *handlers) clusterOverview(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	name := chi.URLParam(r, "cluster")
	transport, err := h.overviewTransport(r.Context(), id, name)
	if err != nil {
		status, message := transportError(err)
		WriteError(r.Context(), w, NewError(status, message))
		return
	}
	scope, err := h.overviewScope(r.Context(), id, name)
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	overview, err := kube.CollectOverview(r.Context(), name, transport, scope)
	if err != nil {
		if errors.Is(err, kube.ErrOverviewDegraded) {
			WriteError(r.Context(), w, NewError(CodeClusterDegraded, "集群可连接但统计信息读取失败，请稍后重试"))
			return
		}
		WriteError(r.Context(), w, NewError(CodeClusterUnreachable, "目标集群不可达，请稍后重试"))
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// overviewTransport 允许仅有命名空间绑定的用户访问总览。
// 资源读取仍通过 Impersonation 由 apiserver 判定，这里只做集群可见性准入。
func (h *handlers) overviewTransport(ctx context.Context, id auth.Identity, name string) (kube.Transport, error) {
	allowed, err := h.clusterVisible(ctx, id, name)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errForbidden
	}
	cluster, ok := h.cluster(name)
	if !ok {
		return nil, domain.ErrNotFound
	}
	if info := cluster.Info(ctx); info.Status == kube.StatusUnreachable {
		return nil, errClusterUnreachable
	}
	return cluster.TransportFor(id.UserRef())
}

// overviewScope 把平台已有的命名空间绑定转换成统计范围。
// 平台管理员或拥有 "*" 绑定的用户不做范围限制，由 apiserver 决定可见范围。
func (h *handlers) overviewScope(ctx context.Context, id auth.Identity, cluster string) (kube.OverviewOptions, error) {
	if id.PlatformAdmin {
		return kube.OverviewOptions{}, nil
	}
	bindings, err := h.bindingsFor(ctx, id)
	if err != nil {
		return kube.OverviewOptions{}, err
	}
	namespaces := make([]string, 0, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.ClusterName != cluster {
			continue
		}
		if binding.Namespace == "*" {
			return kube.OverviewOptions{}, nil
		}
		if binding.Namespace == "" {
			continue
		}
		if _, ok := seen[binding.Namespace]; ok {
			continue
		}
		seen[binding.Namespace] = struct{}{}
		namespaces = append(namespaces, binding.Namespace)
	}
	return kube.OverviewOptions{Namespaces: uniqueSorted(namespaces), NamespacesKnown: true}, nil
}
