// Package kube 封装 Kubernetes 访问：集群传输层、客户端工厂与集群注册表。
package kube

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// 集群可达性状态，与对外 JSON 字段取值保持一致。
const (
	StatusHealthy     = "healthy"
	StatusDegraded    = "degraded"
	StatusUnreachable = "unreachable"
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
	// Name 返回注册名，不触发任何网络请求。
	Name() string
	// Info 返回集群版本与可达性；实现必须自行限制单次探测时长。
	Info(ctx context.Context) ClusterInfo
	Namespaces(ctx context.Context) ([]string, error)
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts ListOptions) (*unstructured.UnstructuredList, error)
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
}
