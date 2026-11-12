// Package api 承载 HTTP 接口层：路由、handler、请求/响应模型。
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/config"
	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/web"
)

// NewRouter 构建 HTTP 路由。
func NewRouter(cfg config.Config, registry *kube.Registry) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(SecurityHeaders)
	r.Use(Recoverer)
	r.Use(Timeout(DefaultRequestTimeout))
	r.Use(CORS(cfg.AllowedOrigins))

	h := &handlers{registry: registry}

	// 进程健康检查；/readyz（依赖 PG 与集群）在数据层就位后加入。
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/clusters", h.listClusters)
		api.Route("/clusters/{cluster}", func(c chi.Router) {
			c.Get("/namespaces", h.listNamespaces)
			c.Get("/resources/{group}/{version}/{resource}", h.listResources)
			c.Get("/resources/{group}/{version}/{resource}/{name}", h.getResource)
		})
	})

	// 前端静态资源（SPA），未匹配的 API 路径返回统一 404。
	r.Handle("/*", SPAHandler(web.Dist()))

	return r
}
