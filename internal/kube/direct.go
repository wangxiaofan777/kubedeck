package kube

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	authzv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// requestTimeout 限制单次 apiserver 请求，避免慢集群拖住整个 handler。
	requestTimeout = 10 * time.Second
	// namespacePageSize 是命名空间分页大小；listAllNamespaces 会跟随 continue 直到取完。
	namespacePageSize = 500
	// maxNamespacePages 是防御性上限，避免异常服务端返回重复游标造成死循环。
	maxNamespacePages = 200
)

var namespaceGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

// DirectCluster 直连目标集群。平台自己的凭据只用于健康探测与 discovery；
// 用户请求通过 TransportFor 构造带 Impersonation 的独立客户端（design D2）。
type DirectCluster struct {
	name   string
	server string
	base   *rest.Config

	mu         sync.Mutex
	transports map[string]*DirectTransport
}

const maxCachedIdentities = 256

// NewDirectClusterFromKubeconfig 从 kubeconfig 构建集群。
// kubeconfig 身份只用于健康探测与 discovery；用户请求由 TransportFor 单独 Impersonation。
func NewDirectClusterFromKubeconfig(name, kubeconfigPath string) (*DirectCluster, error) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}
	return NewDirectCluster(name, cfg)
}

// NewDirectCluster 用现成的 rest.Config 构建集群，供数据库凭据接入复用。
func NewDirectCluster(name string, cfg *rest.Config) (*DirectCluster, error) {
	if name == "" {
		return nil, fmt.Errorf("cluster name must not be empty")
	}
	base := rest.CopyConfig(cfg)
	base.QPS = 50
	base.Burst = 100
	base.Timeout = requestTimeout

	// 健康探测使用平台身份；若 base 自带 Impersonation 则原样保留。
	if _, err := discovery.NewDiscoveryClientForConfig(base); err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	return &DirectCluster{
		name:       name,
		server:     base.Host,
		base:       base,
		transports: make(map[string]*DirectTransport),
	}, nil
}

// Name 返回集群注册名。
func (c *DirectCluster) Name() string { return c.name }

// Server 返回 apiserver 地址。
func (c *DirectCluster) Server() string { return c.server }

// Info 返回集群版本与可达性。底层请求受 rest.Config.Timeout 限制。
func (c *DirectCluster) Info(ctx context.Context) ClusterInfo {
	info := ClusterInfo{Name: c.name, Server: c.server}
	if err := ctx.Err(); err != nil {
		info.Status = StatusUnreachable
		info.Message = err.Error()
		return info
	}
	disc, err := discovery.NewDiscoveryClientForConfig(c.base)
	if err != nil {
		info.Status = StatusUnreachable
		info.Message = err.Error()
		return info
	}
	v, err := disc.ServerVersion()
	if err != nil {
		info.Status = StatusUnreachable
		info.Message = err.Error()
		return info
	}
	info.Version = v.GitVersion
	info.Status = StatusHealthy
	return info
}

// TransportFor 返回带用户身份的传输层；相同身份复用客户端，避免每请求重建 TLS 配置。
func (c *DirectCluster) TransportFor(user UserRef) (Transport, error) {
	key := userKey(user)
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.transports[key]; ok {
		return t, nil
	}

	cfg := rest.CopyConfig(c.base)
	if user.Name != "" {
		cfg.Impersonate = rest.ImpersonationConfig{UserName: user.Name, Groups: user.Groups}
	}
	t, err := newDirectTransport(c.name, c.server, cfg)
	if err != nil {
		return nil, err
	}
	if len(c.transports) >= maxCachedIdentities {
		c.evictOneLocked()
	}
	c.transports[key] = t
	return t, nil
}

func (c *DirectCluster) evictOneLocked() {
	keys := make([]string, 0, len(c.transports))
	for k := range c.transports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	delete(c.transports, keys[0])
}

func userKey(u UserRef) string {
	key := u.Name
	for _, g := range u.Groups {
		key += "\x00" + g
	}
	return key
}

// DirectTransport 是通过 rest.Config 直连的传输层，绑定一个固定用户身份。
type DirectTransport struct {
	name      string
	server    string
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
	authz     authzv1.AuthorizationV1Interface
}

func newDirectTransport(name, server string, cfg *rest.Config) (*DirectTransport, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	authz, err := authzv1.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build authorization client: %w", err)
	}
	return &DirectTransport{
		name:      name,
		server:    server,
		dynamic:   dyn,
		discovery: disc,
		authz:     authz,
	}, nil
}

// Name 返回注册名。
func (t *DirectTransport) Name() string { return t.name }

// Info 返回集群版本与可达性。
func (t *DirectTransport) Info(ctx context.Context) ClusterInfo {
	info := ClusterInfo{Name: t.name, Server: t.server}
	if err := ctx.Err(); err != nil {
		info.Status = StatusUnreachable
		info.Message = err.Error()
		return info
	}
	v, err := t.discovery.ServerVersion()
	if err != nil {
		info.Status = StatusUnreachable
		info.Message = err.Error()
		return info
	}
	info.Version = v.GitVersion
	info.Status = StatusHealthy
	return info
}

// Namespaces 返回全部命名空间名称，跟随分页游标直到取完。
func (t *DirectTransport) Namespaces(ctx context.Context) ([]string, error) {
	return listAllNamespaces(ctx, func(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		return t.dynamic.Resource(namespaceGVR).List(ctx, opts)
	})
}

// List 列出一个 GVR 的资源；namespace 为空表示全部命名空间（仅对命名空间级资源有效）。
func (t *DirectTransport) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts ListOptions) (*unstructured.UnstructuredList, error) {
	return t.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{
		Limit:         opts.Limit,
		Continue:      opts.Continue,
		LabelSelector: opts.LabelSelector,
		FieldSelector: opts.FieldSelector,
	})
}

// Get 读取单个资源。
func (t *DirectTransport) Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	return t.dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// ReviewAccess 用 SelfSubjectAccessReview 判断当前身份是否被 apiserver 允许。
func (t *DirectTransport) ReviewAccess(ctx context.Context, req AccessRequest) (AccessResult, error) {
	review := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace:   req.Namespace,
				Verb:        req.Verb,
				Group:       req.Group,
				Resource:    req.Resource,
				Subresource: req.Subresource,
				Name:        req.Name,
			},
		},
	}
	resp, err := t.authz.SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return AccessResult{}, err
	}
	reason := resp.Status.Reason
	if resp.Status.EvaluationError != "" {
		if reason != "" {
			reason += "; "
		}
		reason += resp.Status.EvaluationError
	}
	return AccessResult{Allowed: resp.Status.Allowed, Reason: reason}, nil
}

// namespaceLister 抽象一次分页查询，便于对分页逻辑做单元测试。
type namespaceLister func(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)

// listAllNamespaces 跟随 continue 游标收集全部命名空间，避免静默截断。
func listAllNamespaces(ctx context.Context, list namespaceLister) ([]string, error) {
	names := make([]string, 0, namespacePageSize)
	opts := metav1.ListOptions{Limit: namespacePageSize}

	for page := 0; ; page++ {
		if page >= maxNamespacePages {
			return nil, fmt.Errorf("namespace list exceeded %d pages", maxNamespacePages)
		}
		result, err := list(ctx, opts)
		if err != nil {
			return nil, err
		}
		for i := range result.Items {
			names = append(names, result.Items[i].GetName())
		}
		next := result.GetContinue()
		if next == "" {
			return names, nil
		}
		opts.Continue = next
	}
}
