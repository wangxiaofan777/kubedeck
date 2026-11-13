package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

const (
	accessCacheTTL  = 30 * time.Second
	maxAccessChecks = 100
)

type accessCheck struct {
	Cluster     string `json:"cluster"`
	Namespace   string `json:"namespace"`
	Verb        string `json:"verb"`
	Group       string `json:"group"`
	Version     string `json:"version"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource"`
	Name        string `json:"name"`
}

type accessResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

type accessCacheItem struct {
	result  accessResult
	expires time.Time
}

type accessCache struct {
	mu    sync.Mutex
	items map[string]accessCacheItem
	now   func() time.Time
}

func newAccessCache() *accessCache {
	return &accessCache{items: make(map[string]accessCacheItem), now: time.Now}
}

func (c *accessCache) get(key string) (accessResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if !ok {
		return accessResult{}, false
	}
	if c.now().After(item.expires) {
		delete(c.items, key)
		return accessResult{}, false
	}
	return item.result, true
}

func (c *accessCache) put(key string, result accessResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, item := range c.items {
		if now.After(item.expires) {
			delete(c.items, k)
		}
	}
	c.items[key] = accessCacheItem{result: result, expires: now.Add(accessCacheTTL)}
}

// can 批量执行权限预检，结果缓存不超过 60 秒（规格上限）。
func (h *handlers) can(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	var body struct {
		Checks []accessCheck `json:"checks"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Checks) == 0 || len(body.Checks) > maxAccessChecks {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument,
			fmt.Sprintf("checks 数量必须在 1-%d 之间", maxAccessChecks)))
		return
	}
	for _, c := range body.Checks {
		if c.Cluster == "" || c.Verb == "" || c.Resource == "" {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "cluster/verb/resource 不能为空"))
			return
		}
	}

	bindings, err := h.bindingsFor(r.Context(), id)
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}

	results := make([]accessResult, len(body.Checks))
	var wg sync.WaitGroup
	for i, check := range body.Checks {
		wg.Add(1)
		go func(i int, check accessCheck) {
			defer wg.Done()
			results[i] = h.reviewAccess(r.Context(), id, bindings, check)
		}(i, check)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *handlers) reviewAccess(ctx context.Context, id auth.Identity, bindings []domain.RoleBinding, check accessCheck) accessResult {
	if !bindingAllows(bindings, check.Cluster, check.Namespace, id.PlatformAdmin) {
		return accessResult{Allowed: false, Reason: "平台未授予该集群或命名空间"}
	}
	cluster, ok := h.cluster(check.Cluster)
	if !ok {
		return accessResult{Allowed: false, Reason: "集群不存在"}
	}
	if !id.PlatformAdmin && cluster.Info(ctx).Status == kube.StatusUnreachable {
		return accessResult{Allowed: false, Reason: "集群不可达"}
	}

	key := accessKey(id, check)
	if result, ok := h.accessCache.get(key); ok {
		return result
	}
	transport, err := cluster.TransportFor(id.UserRef())
	if err != nil {
		return accessResult{Allowed: false, Reason: "无法建立集群连接"}
	}
	result, err := transport.ReviewAccess(ctx, kube.AccessRequest{
		Verb:        check.Verb,
		Group:       check.Group,
		Version:     check.Version,
		Resource:    check.Resource,
		Subresource: check.Subresource,
		Namespace:   check.Namespace,
		Name:        check.Name,
	})
	if err != nil {
		return accessResult{Allowed: false, Reason: "权限预检失败：" + upstreamReason(err)}
	}
	h.accessCache.put(key, accessResult{Allowed: result.Allowed, Reason: result.Reason})
	return accessResult{Allowed: result.Allowed, Reason: result.Reason}
}

func accessKey(id auth.Identity, c accessCheck) string {
	return strings.Join([]string{
		id.Subject,
		strings.Join(id.Groups, ","),
		c.Cluster, c.Namespace, c.Verb, c.Group, c.Version, c.Resource, c.Subresource, c.Name,
	}, "\x00")
}

func upstreamReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// bindingsFor 返回用户绑定；平台管理员不需要平台绑定。
func (h *handlers) bindingsFor(ctx context.Context, id auth.Identity) ([]domain.RoleBinding, error) {
	if id.PlatformAdmin || h.authStore == nil || id.UserID == 0 {
		return nil, nil
	}
	return h.authStore.BindingsForUser(ctx, id.UserID)
}

// bindingAllows 判断平台绑定是否覆盖集群与命名空间。管理员始终允许。
// namespace 为空表示集群级请求，只接受 "*" 绑定。
func bindingAllows(bindings []domain.RoleBinding, cluster, namespace string, admin bool) bool {
	if admin {
		return true
	}
	for _, b := range bindings {
		if b.ClusterName != cluster {
			continue
		}
		if b.Namespace == "*" {
			return true
		}
		if namespace != "" && b.Namespace == namespace {
			return true
		}
	}
	return false
}

// clusterAccess 是资源 handler 的统一准入检查：集群可见 + 命名空间在绑定范围内。
func (h *handlers) clusterAccess(ctx context.Context, id auth.Identity, cluster, namespace string) (bool, error) {
	if id.PlatformAdmin {
		return true, nil
	}
	bindings, err := h.bindingsFor(ctx, id)
	if err != nil {
		return false, err
	}
	return bindingAllows(bindings, cluster, namespace, false), nil
}

// clusterVisible 判断用户是否可访问某集群的集群级只读信息（发现、健康等）：
// 管理员，或在该集群上存在任意一条绑定。它与 clusterAccess 的区别是不要求
// 绑定覆盖具体命名空间；跨命名空间的资源请求仍必须走 clusterAccess。
func (h *handlers) clusterVisible(ctx context.Context, id auth.Identity, cluster string) (bool, error) {
	if id.PlatformAdmin {
		return true, nil
	}
	bindings, err := h.bindingsFor(ctx, id)
	if err != nil {
		return false, err
	}
	for _, b := range bindings {
		if b.ClusterName == cluster {
			return true, nil
		}
	}
	return false, nil
}

func (h *handlers) cluster(name string) (kube.ClusterAccess, bool) {
	if h.registry == nil {
		return nil, false
	}
	return h.registry.Get(name)
}
