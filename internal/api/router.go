// Package api 承载 HTTP 接口层：路由、handler、请求/响应模型。
package api

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/webui"
)

// ClusterRegistry 是 handler 对集群注册表的最小依赖。
type ClusterRegistry interface {
	Get(name string) (kube.ClusterAccess, bool)
	All() []kube.ClusterAccess
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
	Auth           AuthService
	AuthStore      AuthStore
	AdminStore     AdminStore
	ClusterAdmin   ClusterAdmin
	Audit          AuditWriter
	AuditStore     AuditStore
	BootstrapToken string
	CookieSecure   bool
}

type handlers struct {
	registry       ClusterRegistry
	pinger         Pinger
	authSvc        AuthService
	authStore      AuthStore
	adminStore     AdminStore
	clusterAdmin   ClusterAdmin
	audit          AuditWriter
	auditStore     AuditStore
	bootstrapToken string
	cookieSecure   bool
	accessCache    *accessCache
	ops            *operationStore
	idem           *idempotencyStore
	allowedOrigins []string
	wsMu           sync.Mutex
	wsCount        map[int64]int
}

// NewRouter 构建 HTTP 路由。
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()

	r.Use(RequestID)
	r.Use(SecurityHeaders)
	r.Use(Recoverer)
	r.Use(metricsMiddleware)
	r.Use(Timeout(DefaultRequestTimeout))
	r.Use(CORS(opts.AllowedOrigins))
	r.Use(sameOrigin(opts.AllowedOrigins))

	h := &handlers{
		registry:       opts.Registry,
		pinger:         opts.Pinger,
		authSvc:        opts.Auth,
		authStore:      opts.AuthStore,
		adminStore:     opts.AdminStore,
		clusterAdmin:   opts.ClusterAdmin,
		audit:          opts.Audit,
		auditStore:     opts.AuditStore,
		bootstrapToken: opts.BootstrapToken,
		cookieSecure:   opts.CookieSecure,
		accessCache:    newAccessCache(),
		ops:            newOperationStore(),
		idem:           newIdempotencyStore(),
		allowedOrigins: opts.AllowedOrigins,
		wsCount:        make(map[int64]int),
	}

	r.Get("/healthz", h.healthz)
	r.Get("/readyz", h.readyz)
	r.Get("/metrics", h.metricsHandler)

	// OIDC 跳转与回调必须位于未认证路由。
	r.Get("/auth/login", h.login)
	r.Get("/auth/callback", h.callback)
	r.Post("/auth/logout", h.logout)
	r.Post("/auth/bootstrap", h.bootstrap)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(h.requireIdentity)
		api.Use(h.auditRequests)

		api.Get("/auth/me", h.me)
		api.Post("/auth/can", h.can)
		api.Post("/auth/logout", h.logout)

		api.Get("/clusters", h.listClusters)
		api.With(h.requireAdmin).Post("/clusters", h.createCluster)
		api.Route("/clusters/{cluster}", func(c chi.Router) {
			c.Get("/", h.getCluster)
			c.Get("/health", h.getClusterHealth)
			c.Get("/overview", h.clusterOverview)
			c.Get("/api-resources", h.apiResources)
			c.Get("/namespaces", h.listNamespaces)
			c.Get("/resources/{group}/{version}/{resource}", h.listResources)
			c.Post("/resources/{group}/{version}/{resource}", h.createResource)
			c.Delete("/resources/{group}/{version}/{resource}", h.batchDelete)
			c.Get("/resources/{group}/{version}/{resource}/{name}", h.getResource)
			c.Put("/resources/{group}/{version}/{resource}/{name}", h.updateResource)
			c.Patch("/resources/{group}/{version}/{resource}/{name}", h.patchResource)
			c.Delete("/resources/{group}/{version}/{resource}/{name}", h.deleteResource)
			c.Get("/resources/{group}/{version}/{resource}/{name}/detail", h.getResourceDetail)
			c.Post("/resources/{group}/{version}/{resource}/{name}/diff", h.diffResource)
			c.Post("/workloads/{group}/{version}/{resource}/{name}/scale", h.scaleWorkload)
			c.Post("/workloads/{group}/{version}/{resource}/{name}/restart", h.restartWorkload)
			c.Get("/workloads/{group}/{version}/{resource}/{name}/history", h.workloadHistory)
			c.Get("/workloads/{group}/{version}/{resource}/{name}/rollback/diff", h.rollbackDiff)
			c.Post("/workloads/{group}/{version}/{resource}/{name}/rollback", h.rollbackWorkload)
			c.Post("/workloads/{group}/{version}/{resource}/{name}/pause", h.pauseWorkload)
			c.Post("/workloads/{group}/{version}/{resource}/{name}/resume", h.resumeWorkload)
			c.Post("/pods/{namespace}/{name}/evict", h.evictPod)
			c.Get("/pods/{namespace}/{name}/log", h.podLog)
			c.Get("/events", h.listEvents)
			c.Get("/operations/{id}", h.getOperation)
			c.Get("/stream", h.stream)
			c.With(h.requireAdmin).Patch("/", h.patchCluster)
			c.With(h.requireAdmin).Delete("/", h.deleteCluster)
			c.With(h.requireAdmin).Get("/credential", h.getCredential)
			c.With(h.requireAdmin).Put("/credential", h.rotateCredential)
		})

		api.Route("/admin", func(a chi.Router) {
			a.Use(h.requireAdmin)
			a.Get("/users", h.listUsers)
			a.Post("/users/{id}/status", h.setUserStatus)
			a.Get("/roles", h.listRoles)
			a.Get("/clusters", h.listAdminClusters)
			a.Get("/role-bindings", h.listRoleBindings)
			a.Put("/role-bindings", h.putUserAccess)
			a.Delete("/role-bindings/{id}", h.deleteRoleBinding)
			a.Get("/audit-logs", h.listAuditLogs)
			a.Get("/audit-logs/verify", h.verifyAuditChain)
			a.Get("/audit-logs/export", h.exportAuditLogs)
		})
	})

	// 前端静态资源（SPA），未匹配的 API 路径返回统一 404。
	r.Handle("/*", SPAHandler(webui.Dist()))

	return r
}
