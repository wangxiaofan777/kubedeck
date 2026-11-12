package kube

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
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

// DirectTransport 通过 kubeconfig 直连目标集群的 apiserver。
type DirectTransport struct {
	name      string
	server    string
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface
}

// NewDirectTransport 从 kubeconfig 构建直连传输层。
// impersonateUser 非空时，所有请求携带 Impersonate-User（本地验证 RBAC 用）。
func NewDirectTransport(name, kubeconfigPath, impersonateUser string) (*DirectTransport, error) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}
	if impersonateUser != "" {
		cfg.Impersonate = rest.ImpersonationConfig{UserName: impersonateUser}
	}
	cfg.QPS = 50
	cfg.Burst = 100
	cfg.Timeout = requestTimeout

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	return &DirectTransport{
		name:      name,
		server:    cfg.Host,
		dynamic:   dyn,
		discovery: disc,
	}, nil
}

// Name 返回注册名。
func (t *DirectTransport) Name() string { return t.name }

// Info 返回集群版本与可达性。底层请求受 rest.Config.Timeout 限制。
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
