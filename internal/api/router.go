// Package api 承载 HTTP 接口层：路由、handler、请求/响应模型。
package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/webui"
)

// ClusterRegistry 是 handler 对集群注册表的最小依赖。
type ClusterRegistry interface {
	Get(name string) (kube.Transport, bool)
	All() []kube.Transport
}

// Pinger 是就绪探测对平台依赖的最小依赖。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options 是路由装配参数。零值表示无集群、无数据库。
type Options struct {
	AllowedOrigins []string
	Registry       ClusterRegistry
	Pinger         Pinger
}

type handlers struct {
	registry ClusterRegistry
	pinger   Pinger
}

// NewRouter 构建 HTTP 路由。
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(SecurityHeaders)
	r.Use(Recoverer)
	r.Use(Timeout(DefaultRequestTimeout))
	r.Use(CORS(opts.AllowedOrigins))

	h := &handlers{registry: opts.Registry, pinger: opts.Pinger}

	r.Get("/healthz", h.healthz)
	r.Get("/readyz", h.readyz)

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/clusters", h.listClusters)
		api.Route("/clusters/{cluster}", func(c chi.Router) {
			c.Get("/namespaces", h.listNamespaces)
			c.Get("/resources/{group}/{version}/{resource}", h.listResources)
			c.Get("/resources/{group}/{version}/{resource}/{name}", h.getResource)
		})
	})

	// 前端静态资源（SPA），未匹配的 API 路径返回统一 404。
	r.Handle("/*", SPAHandler(webui.Dist()))

	return r
}
