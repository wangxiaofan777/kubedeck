// Package kube 封装 Kubernetes 访问：ClusterTransport、client 工厂、informer 缓存。
package kube

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ClusterInfo 是对外暴露的集群摘要。
type ClusterInfo struct {
	Name    string `json:"name"`
	Server  string `json:"server"`
	Version string `json:"version"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ListOptions 是资源列表查询参数。
type ListOptions struct {
	Limit         int64
	Continue      string
	LabelSelector string
	FieldSelector string
}

// Transport 抽象对单个集群的访问。Direct 是本期唯一实现，Agent 模式后续扩展。
type Transport interface {
	Info(ctx context.Context) ClusterInfo
	Namespaces(ctx context.Context) ([]string, error)
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts ListOptions) (*unstructured.UnstructuredList, error)
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
}

// Registry 维护集群名到 Transport 的映射。开发模式下只注册一个本地集群；
// 生产模式后续由数据库中的集群登记驱动。
type Registry struct {
	mu    sync.RWMutex
	items map[string]Transport
	order []string
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{items: make(map[string]Transport)}
}

// Add 注册集群；同名覆盖。
func (r *Registry) Add(t Transport) {
	info := t.Info(context.Background())
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[info.Name]; !exists {
		r.order = append(r.order, info.Name)
	}
	r.items[info.Name] = t
}

// Get 按名称查找集群。
func (r *Registry) Get(name string) (Transport, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[name]
	return t, ok
}

// All 按注册顺序返回全部集群。
func (r *Registry) All() []Transport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Transport, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.items[name]; ok {
			out = append(out, t)
		}
	}
	return out
}
