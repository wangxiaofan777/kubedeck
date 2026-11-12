// Package api 承载 HTTP 接口层：路由、handler、请求/响应模型。
package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DefaultRequestTimeout 是单个请求的处理超时。
const DefaultRequestTimeout = 30 * time.Second

// RequestID 为每个请求分配唯一 ID：沿用合法的客户端/网关 X-Request-Id，
// 否则重新生成；写入 context 并回写响应头。
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
	})
}

// validRequestID 限制外部传入 ID 的长度与字符集，避免响应头与日志注入。
func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// Recoverer 捕获 handler panic：记录堆栈、返回统一 INTERNAL 错误，保证进程不退出。
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"requestId", RequestIDFrom(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				WriteError(r.Context(), w, NewError(CodeInternal, "平台内部错误"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Timeout 为请求上下文设置超时；handler 必须尊重 ctx.Done()。
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	if d <= 0 {
		d = DefaultRequestTimeout
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// WebSocket 是长连接，不能套用请求级超时；由连接心跳与读超时管理。
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CORS 按白名单放行跨域请求。白名单为空时不产生任何 CORS 头（仅同源）。
// 开发环境由 Vite 的 server.proxy 代理，生产环境由同域 Ingress 承载，通常无需开启。
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowed[origin]; ok && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, Idempotency-Key")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "600")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders 设置基础安全响应头。
// Ant Design 5 在运行时注入 <style>，因此 style-src 需要 'unsafe-inline'；
// 脚本仍限制为同源，不使用 unsafe-inline/unsafe-eval。
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		next.ServeHTTP(w, r)
	})
}
