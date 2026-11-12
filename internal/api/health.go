package api

import (
	"context"
	"net/http"
	"time"
)

const readinessTimeout = 2 * time.Second

// healthz 表示进程存活，不检查依赖。
func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

// readyz 表示可以接收流量：配置了数据库时检查其连通性。
func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	if h.pinger == nil {
		writePlain(w, http.StatusOK, "ok")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := h.pinger.Ping(ctx); err != nil {
		writePlain(w, http.StatusServiceUnavailable, "dependency unavailable")
		return
	}
	writePlain(w, http.StatusOK, "ok")
}
