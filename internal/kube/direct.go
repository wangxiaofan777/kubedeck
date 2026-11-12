package kube

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

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

// Info 返回集群版本与可达性。
func (t *DirectTransport) Info(ctx context.Context) ClusterInfo {
	info := ClusterInfo{Name: t.name, Server: t.server}
	v, err := t.discovery.ServerVersion()
	if err != nil {
		info.Status = "unreachable"
		info.Message = err.Error()
		return info
	}
	info.Version = v.GitVersion
	info.Status = "healthy"
	return info
}

// Namespaces 返回全部命名空间名称。
func (t *DirectTransport) Namespaces(ctx context.Context) ([]string, error) {
	list, err := t.dynamic.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).
		List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}
	return names, nil
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
