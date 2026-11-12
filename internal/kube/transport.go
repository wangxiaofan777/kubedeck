// Package kube 封装 Kubernetes 访问：集群传输层、客户端工厂与集群注册表。
package kube

import (
	"context"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// 集群可达性状态，与对外 JSON 字段取值保持一致。
const (
	StatusHealthy     = "healthy"
	StatusDegraded    = "degraded"
	StatusUnreachable = "unreachable"
	StatusUnknown     = "unknown"
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

// UserRef 是发起集群请求的用户身份。所有资源请求都必须带上它，
// 由 apiserver 通过 Impersonation 完成授权（design D2）。
type UserRef struct {
	Name   string
	Groups []string
}

// AccessRequest 描述一次权限预检。
type AccessRequest struct {
	Verb        string
	Group       string
	Version     string
	Resource    string
	Subresource string
	Namespace   string
	Name        string
}

// AccessResult 是权限预检结果。
type AccessResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Transport 抽象某个用户视角下的单集群访问。Direct 是本期唯一实现，Agent 模式后续扩展。
// 实现必须把用户身份传递给 apiserver，不得退化成平台自身的高权限身份。
type Transport interface {
	// Name 返回注册名，不触发任何网络请求。
	Name() string
	// Info 返回集群版本与可达性；实现必须自行限制单次探测时长。
	Info(ctx context.Context) ClusterInfo
	Namespaces(ctx context.Context) ([]string, error)
	List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts ListOptions) (*unstructured.UnstructuredList, error)
	Get(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error)
	// ReviewAccess 以当前用户身份执行 SelfSubjectAccessReview。
	ReviewAccess(ctx context.Context, req AccessRequest) (AccessResult, error)
	Create(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, dryRun []string) (*unstructured.Unstructured, error)
	Update(ctx context.Context, gvr schema.GroupVersionResource, namespace string, obj *unstructured.Unstructured, dryRun []string) (*unstructured.Unstructured, error)
	Delete(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, opts DeleteOptions) error
	// Scale 通过 scale 子资源调整副本数，避免与主体更新冲突。
	Scale(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, replicas int32) (*unstructured.Unstructured, error)
	// Evict 通过 policy/v1 Eviction 驱逐 Pod，受 PDB 约束。
	Evict(ctx context.Context, namespace, name string, gracePeriodSeconds *int64) error
	// Logs 读取或跟随 Pod 容器日志。调用方负责关闭返回的 reader。
	Logs(ctx context.Context, namespace, name string, opts LogOptions) (io.ReadCloser, error)
}

// LogOptions 是 Pod 日志读取参数。
type LogOptions struct {
	Container    string
	TailLines    *int64
	SinceSeconds *int64
	SinceTime    string
	Previous     bool
	Follow       bool
	Timestamps   bool
}

// ClusterAccess 是注册表持有的集群入口：既能给出与用户无关的健康信息，
// 也能按用户身份构造独立传输层。
type ClusterAccess interface {
	Name() string
	Info(ctx context.Context) ClusterInfo
	TransportFor(user UserRef) (Transport, error)
	Discovery(ctx context.Context) (DiscoveryResult, error)
}
