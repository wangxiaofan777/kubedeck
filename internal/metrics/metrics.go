// Package metrics 暴露平台自身的 Prometheus 指标。
package metrics

import "github.com/prometheus/client_golang/prometheus"
import "github.com/prometheus/client_golang/prometheus/promauto"

var (
	// HTTPRequests 按方法、路由模式与状态码统计请求量。
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kubedeck_http_requests_total",
		Help: "Total HTTP requests by method, route and status.",
	}, []string{"method", "route", "status"})

	// HTTPDuration 统计请求延迟，用于列表 P95 与错误率告警。
	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kubedeck_http_request_duration_seconds",
		Help:    "HTTP request latency by method and route.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.3, 0.5, 1, 2, 5, 10},
	}, []string{"method", "route"})

	// WSConnections 是当前活跃 WebSocket 连接数。
	WSConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubedeck_ws_connections",
		Help: "Current active WebSocket connections.",
	})

	// WSResync 统计无法续传或慢消费者触发的全量重同步。
	WSResync = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kubedeck_ws_resync_total",
		Help: "WebSocket resync events by reason.",
	}, []string{"reason"})

	// AuditQueue 是审计队列中待发送的记录数。
	AuditQueue = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubedeck_audit_queue_depth",
		Help: "Pending audit entries in the async queue.",
	})

	// AuditRetry 是等待重试的审计记录数。
	AuditRetry = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubedeck_audit_retry_depth",
		Help: "Audit entries waiting for retry.",
	})

	// AuditDropped 是因队列溢出被丢弃的审计记录累计数。
	AuditDropped = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubedeck_audit_dropped_total",
		Help: "Audit entries dropped because the queue was full.",
	})

	// AuditDegraded 表示审计写入是否处于降级状态。
	AuditDegraded = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "kubedeck_audit_degraded",
		Help: "1 when audit writes are degraded, 0 otherwise.",
	})

	// ClusterUp 是最近一次健康探测结果，1 表示 healthy。
	ClusterUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kubedeck_cluster_up",
		Help: "Last cluster health probe result (1 healthy, 0 otherwise).",
	}, []string{"cluster"})
)
