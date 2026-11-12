package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

func identityFromRequest(w http.ResponseWriter, r *http.Request) (auth.Identity, bool) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
	}
	return id, ok
}

// accessibleCluster 执行平台准入并返回集群入口。
func (h *handlers) accessibleCluster(w http.ResponseWriter, r *http.Request, id auth.Identity, namespace string) (kube.ClusterAccess, bool) {
	name := chi.URLParam(r, "cluster")
	allowed, err := h.clusterAccess(r.Context(), id, name, namespace)
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return nil, false
	}
	if !allowed {
		WriteError(r.Context(), w, NewError(CodeForbidden, "无权访问该集群或命名空间"))
		return nil, false
	}
	cluster, ok := h.cluster(name)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return nil, false
	}
	return cluster, true
}

// validGVR 解析并校验资源三元组；不在发现结果中时返回 400 与可用类型提示。
func (h *handlers) validGVR(w http.ResponseWriter, r *http.Request, cluster kube.ClusterAccess) (schema.GroupVersionResource, bool) {
	gvr, ok := gvrFromPath(r)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "group/version/resource 不能为空"))
		return schema.GroupVersionResource{}, false
	}
	discovery, err := cluster.Discovery(r.Context())
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeClusterUnreachable, "资源发现失败，请稍后重试"))
		return schema.GroupVersionResource{}, false
	}
	for _, res := range discovery.Resources {
		if res.GVR() == gvr {
			return gvr, true
		}
	}
	e := NewError(CodeInvalidArgument, fmt.Sprintf("资源类型 %s 不存在", gvrLabel(gvr)))
	e.Details = map[string]any{"available": resourceHints(discovery.Resources)}
	WriteError(r.Context(), w, e)
	return schema.GroupVersionResource{}, false
}

func gvrLabel(gvr schema.GroupVersionResource) string {
	group := gvr.Group
	if group == "" {
		group = "_"
	}
	return group + "/" + gvr.Version + "/" + gvr.Resource
}

func resourceHints(resources []kube.APIResource) []string {
	limit := len(resources)
	if limit > 20 {
		limit = 20
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, gvrLabel(resources[i].GVR()))
	}
	return out
}

// validObjectName 拒绝路径控制字符，避免拼接进上游 URL。
func validObjectName(w http.ResponseWriter, ctx context.Context, name string) bool {
	if name == "" || len(name) > 253 || strings.Contains(name, "/") ||
		strings.Contains(name, "..") || strings.ContainsAny(name, "\x00\n\r\t") {
		WriteError(ctx, w, NewError(CodeInvalidArgument, "对象名不合法"))
		return false
	}
	return true
}

func resourceGVR(group, version, resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}
