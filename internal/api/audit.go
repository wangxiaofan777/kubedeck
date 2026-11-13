package api

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
)

// AuditWriter 是审计队列的最小依赖。
type AuditWriter interface {
	Record(entry domain.AuditEntry) bool
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type bodyHasher struct {
	rc   io.ReadCloser
	sum  hash.Hash
	read int64
}

func (b *bodyHasher) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.sum.Write(p[:n])
		b.read += int64(n)
	}
	return n, err
}

func (b *bodyHasher) Close() error { return b.rc.Close() }

func (b *bodyHasher) digest() string {
	if b.read == 0 {
		return ""
	}
	return hex.EncodeToString(b.sum.Sum(nil))
}

// auditRequests 为写操作与敏感读建立审计记录；只保存请求体摘要，不保存内容。
func (h *handlers) auditRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.audit == nil || !shouldAuditRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		hasher := &bodyHasher{rc: r.Body, sum: sha256.New()}
		if r.Body != nil {
			r.Body = hasher
		}
		start := time.Now()
		next.ServeHTTP(sw, r)
		h.recordRequestAudit(r, sw.status, hasher.digest(), time.Since(start))
	})
}

func shouldAuditRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return isSensitiveRead(r.URL.Path)
	}
	return !strings.HasPrefix(r.URL.Path, "/api/v1/auth/can")
}

// isSensitiveRead 识别 Secret 明文读取；列表浏览不入操作审计。
func isSensitiveRead(path string) bool {
	segments := splitPath(path)
	for i := 0; i+3 < len(segments); i++ {
		if segments[i] == "resources" && segments[i+3] == "secrets" && i+4 < len(segments) {
			return true
		}
	}
	return false
}

func (h *handlers) recordRequestAudit(r *http.Request, status int, bodyHash string, elapsed time.Duration) {
	id, _ := auth.IdentityFrom(r.Context())
	verb, group, resource, namespace, name := auditTarget(r)
	entry := domain.AuditEntry{
		OccurredAt:   time.Now().UTC(),
		RequestID:    RequestIDFrom(r.Context()),
		ActorSubject: id.Subject,
		ActorEmail:   id.Email,
		ClusterName:  auditCluster(r.URL.Path),
		Verb:         verb,
		APIGroup:     group,
		Resource:     resource,
		Namespace:    namespace,
		ObjectName:   name,
		ResultCode:   status,
		LatencyMS:    int(elapsed.Milliseconds()),
		ClientIP:     clientIP(r),
		UserAgent:    r.UserAgent(),
		BodyHash:     bodyHash,
		Detail: map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
		},
	}
	if id.UserID > 0 {
		uid := id.UserID
		entry.ActorID = &uid
	}
	if status >= 400 {
		entry.Result = "failure"
	} else {
		entry.Result = "success"
	}
	if r.URL.Query().Get("dryRun") == "All" {
		entry.Detail["dryRun"] = true
	}
	if isSensitiveRead(r.URL.Path) {
		entry.Detail["sensitiveRead"] = true
	}
	h.audit.Record(entry)
}

func auditCluster(path string) string {
	segments := splitPath(path)
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == "clusters" {
			return segments[i+1]
		}
	}
	return ""
}

func auditTarget(r *http.Request) (verb, group, resource, namespace, name string) {
	segments := splitPath(r.URL.Path)
	namespace = r.URL.Query().Get("namespace")
	verb = strings.ToLower(r.Method)

	// /clusters/{c}/resources/{group}/{version}/{resource}[/{name}]
	for i := 0; i+3 < len(segments); i++ {
		if segments[i] != "resources" {
			continue
		}
		group = segments[i+1]
		if group == "_" {
			group = ""
		}
		resource = segments[i+3]
		if i+4 < len(segments) {
			name = segments[i+4]
		}
		switch r.Method {
		case http.MethodPut:
			verb = "update"
		case http.MethodPost:
			verb = "create"
		case http.MethodPatch:
			verb = "patch"
		case http.MethodDelete:
			if name == "" {
				verb = "deletecollection"
			} else {
				verb = "delete"
			}
		case http.MethodGet:
			verb = "get"
		}
		return verb, group, resource, namespace, name
	}

	// /clusters/{c}/workloads/{group}/{version}/{resource}/{name}/{action}
	for i := 0; i+5 < len(segments); i++ {
		if segments[i] != "workloads" {
			continue
		}
		group = segments[i+1]
		if group == "_" {
			group = ""
		}
		resource = segments[i+3]
		name = segments[i+4]
		action := segments[i+5]
		switch action {
		case "scale":
			verb = "scale"
		case "restart":
			verb = "restart"
		case "rollback":
			verb = "rollback"
		case "pause", "resume":
			verb = "patch"
		default:
			verb = strings.ToLower(r.Method)
		}
		return verb, group, resource, namespace, name
	}

	// /clusters/{c}/pods/{namespace}/{name}/{action}
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] != "pods" {
			continue
		}
		namespace = segments[i+1]
		name = segments[i+2]
		if i+3 < len(segments) && segments[i+3] == "evict" {
			verb = "evict"
		}
		return verb, "", "pods", namespace, name
	}

	// 集群与平台管理接口
	path := r.URL.Path
	switch {
	case strings.Contains(path, "/credential"):
		verb = "credential.rotate"
		resource = "cluster_credentials"
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/clusters"):
		verb = "create"
		resource = "clusters"
	case r.Method == http.MethodDelete && strings.Contains(path, "/clusters/"):
		verb = "delete"
		resource = "clusters"
	case strings.Contains(path, "/admin/role-bindings"):
		resource = "role_bindings"
		if r.Method == http.MethodDelete {
			verb = "revoke"
		} else {
			verb = "grant"
		}
	case strings.Contains(path, "/admin/users"):
		resource = "users"
		verb = "user.status"
	case strings.HasSuffix(path, "/auth/logout"):
		verb = "logout"
		resource = "sessions"
	}
	return verb, group, resource, namespace, name
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
