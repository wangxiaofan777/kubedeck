package api

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kubedeck/kubedeck/internal/metrics"
)

// metricsHandler 暴露平台自身指标。该端点不鉴权，应只在内网暴露。
func (h *handlers) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if h.audit != nil {
		if stats, ok := h.audit.(interface {
			Stats() (queued, retry, dropped int, degraded bool)
		}); ok {
			queued, retry, dropped, degraded := stats.Stats()
			metrics.AuditQueue.Set(float64(queued))
			metrics.AuditRetry.Set(float64(retry))
			metrics.AuditDropped.Set(float64(dropped))
			value := 0.0
			if degraded {
				value = 1
			}
			metrics.AuditDegraded.Set(value)
		}
	}
	promhttp.Handler().ServeHTTP(w, r)
}

// metricsMiddleware 记录请求量与延迟。路由标签使用 chi 的模式，避免高基数。
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		mw := &metricsWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(mw, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		metrics.HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(mw.status)).Inc()
		metrics.HTTPDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// metricsWriter 保留 Hijacker/Flusher，避免 WebSocket 与日志流被中间件包装后失效。
type metricsWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *metricsWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *metricsWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *metricsWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("response writer does not support hijacking")
}

func (w *metricsWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
