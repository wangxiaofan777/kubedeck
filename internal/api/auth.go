package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
)

const (
	sessionCookie = "kubedeck_session"
	stateCookie   = "kubedeck_oidc_state"
)

// AuthService 是 handler 对认证服务的最小依赖。
type AuthService interface {
	DevMode() bool
	DevIdentity() (auth.Identity, bool)
	NewDevSession() (auth.Session, error)
	StartLogin(returnTo string) (authURL, state string, err error)
	FinishLogin(ctx context.Context, state, cookieState, code string) (auth.Session, string, error)
	Session(id string) (auth.Session, bool)
	Logout(id string)
	LogoutUser(userID int64)
}

// AuthStore 是登录态展示与权限管理所需的仓储能力。
type AuthStore interface {
	BindingsForUser(ctx context.Context, userID int64) ([]domain.RoleBinding, error)
	ClustersForUser(ctx context.Context, userID int64) ([]domain.Cluster, error)
}

// AdminStore 是平台管理员接口所需的最小仓储能力。
type AdminStore interface {
	AuthStore
	BootstrapAdmin(ctx context.Context, subject, email, displayName string) (domain.User, error)
	SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error)
	AllRoleBindings(ctx context.Context) ([]domain.RoleBinding, error)
	SetClusterAccess(ctx context.Context, userID, clusterID int64, bindings []domain.RoleBindingInput, grantedBy *int64) error
	DeleteRoleBinding(ctx context.Context, id int64) error
	Roles(ctx context.Context) ([]domain.Role, error)
	ClusterByID(ctx context.Context, id int64) (domain.Cluster, error)
	Clusters(ctx context.Context) ([]domain.Cluster, error)
	SetUserStatus(ctx context.Context, id int64, status string) error
}

// sessionCookie 的 Secure 属性由配置决定；HttpOnly 与 SameSite=Lax 固定。
func (h *handlers) setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *handlers) clearSessionCookie(w http.ResponseWriter) {
	h.setSessionCookie(w, "", -1)
}

// login 跳转到 IdP；returnTo 只允许站内相对路径。
func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	if h.authSvc == nil || h.authSvc.DevMode() {
		// 开发模式不需要登录；直接把用户送回页面。
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	authURL, state, err := h.authSvc.StartLogin(r.URL.Query().Get("returnTo"))
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "身份提供方暂不可用，请稍后重试"))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/auth",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// callback 处理授权码回调，建立会话后回到登录前页面。
func (h *handlers) callback(w http.ResponseWriter, r *http.Request) {
	if h.authSvc == nil {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "未配置登录方式"))
		return
	}
	state := r.URL.Query().Get("state")
	cookieState := ""
	if c, err := r.Cookie(stateCookie); err == nil {
		cookieState = c.Value
	}
	sess, returnTo, err := h.authSvc.FinishLogin(r.Context(), state, cookieState, r.URL.Query().Get("code"))
	if err != nil {
		status := CodeUnauthenticated
		if errors.Is(err, auth.ErrInvalidState) {
			status = CodeInvalidArgument
		}
		WriteError(r.Context(), w, NewError(status, "登录失败，请重新发起登录"))
		return
	}
	h.setSessionCookie(w, sess.ID, int(time.Until(time.Unix(sess.ExpiresAt, 0)).Seconds()))
	h.auditAuthEvent(r, sess.Identity, "login", http.StatusFound)
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/auth", MaxAge: -1, HttpOnly: true, Secure: h.cookieSecure, SameSite: http.SameSiteLaxMode})
	if returnTo == "" {
		returnTo = "/"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// logout 使当前会话立即失效。
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if sid := sessionID(r); sid != "" && h.authSvc != nil {
		if sess, ok := h.authSvc.Session(sid); ok && !strings.HasPrefix(r.URL.Path, "/api/") {
			h.auditAuthEvent(r, sess.Identity, "logout", http.StatusNoContent)
		}
		h.authSvc.Logout(sid)
	}
	h.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// me 返回当前用户、可见集群与绑定；前端用它初始化权限上下文。
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	resp := map[string]any{
		"user": id,
		"mode": "oidc",
	}
	if id.Dev {
		resp["mode"] = "dev"
	}
	resp["clusters"] = []string{}
	resp["bindings"] = []domain.RoleBinding{}
	if id.PlatformAdmin && h.registry != nil {
		names := make([]string, 0)
		for _, c := range h.registry.All() {
			names = append(names, c.Name())
		}
		resp["clusters"] = names
	}
	if h.authStore != nil && id.UserID > 0 {
		if bindings, err := h.authStore.BindingsForUser(r.Context(), id.UserID); err == nil {
			resp["bindings"] = bindings
		}
		if !id.PlatformAdmin {
			if clusters, err := h.authStore.ClustersForUser(r.Context(), id.UserID); err == nil {
				names := make([]string, 0, len(clusters))
				for _, c := range clusters {
					names = append(names, c.Name)
				}
				resp["clusters"] = names
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// bootstrap 创建首个平台管理员；成功后该接口因管理员已存在而返回 409。
func (h *handlers) bootstrap(w http.ResponseWriter, r *http.Request) {
	if h.bootstrapToken == "" || h.adminStore == nil {
		WriteError(r.Context(), w, NewError(CodeNotFound, "引导流程未启用"))
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Bootstrap-Token"))
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if token == "" || token != h.bootstrapToken {
		WriteError(r.Context(), w, NewError(CodeForbidden, "引导令牌无效"))
		return
	}

	var body struct {
		Subject     string `json:"subject"`
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Subject == "" {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "subject 不能为空"))
		return
	}
	user, err := h.adminStore.BootstrapAdmin(r.Context(), body.Subject, body.Email, body.DisplayName)
	switch {
	case errors.Is(err, domain.ErrConflict):
		WriteError(r.Context(), w, NewError(CodeConflict, "平台管理员已存在，引导流程已关闭"))
	case errors.Is(err, domain.ErrInvalid):
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "subject 不能为空"))
	case err != nil:
		WriteError(r.Context(), w, FromPlatformError(err))
	default:
		h.auditAuthEvent(r, auth.Identity{
			UserID: user.ID, Subject: user.Subject, Email: user.Email,
			DisplayName: user.DisplayName, PlatformAdmin: user.IsPlatformAdmin,
		}, "bootstrap", http.StatusCreated)
		writeJSON(w, http.StatusCreated, user)
	}
}

// auditAuthEvent 记录登录/登出/引导等身份事件。
func (h *handlers) auditAuthEvent(r *http.Request, id auth.Identity, verb string, status int) {
	if h.audit == nil {
		return
	}
	entry := domain.AuditEntry{
		OccurredAt:   time.Now().UTC(),
		RequestID:    RequestIDFrom(r.Context()),
		ActorSubject: id.Subject,
		ActorEmail:   id.Email,
		Verb:         verb,
		Resource:     "sessions",
		Result:       "success",
		ResultCode:   status,
		ClientIP:     clientIP(r),
		UserAgent:    r.UserAgent(),
		Detail:       map[string]any{},
	}
	if id.UserID > 0 {
		uid := id.UserID
		entry.ActorID = &uid
	}
	if verb == "bootstrap" {
		entry.Resource = "users"
	}
	h.audit.Record(entry)
}

// requireIdentity 是 API 鉴权中间件。开发模式下首次请求自动建立本地会话。
func (h *handlers) requireIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.authSvc == nil {
			WriteError(r.Context(), w, NewError(CodeUnauthenticated, "未配置登录方式"))
			return
		}
		sid := sessionID(r)
		if sess, ok := h.authSvc.Session(sid); ok {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), sess.Identity)))
			return
		}
		if h.authSvc.DevMode() {
			sess, err := h.authSvc.NewDevSession()
			if err == nil {
				h.setSessionCookie(w, sess.ID, int(time.Until(time.Unix(sess.ExpiresAt, 0)).Seconds()))
				h.auditAuthEvent(r, sess.Identity, "login", http.StatusOK)
				next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), sess.Identity)))
				return
			}
		}
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
	})
}

// requireAdmin 只放行平台管理员。
func (h *handlers) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.IdentityFrom(r.Context())
		if !ok || !id.PlatformAdmin {
			WriteError(r.Context(), w, NewError(CodeForbidden, "需要平台管理员权限"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sessionID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// sameOrigin 拒绝跨站写请求，配合 SameSite=Lax 抵御 CSRF。
func sameOrigin(allowed []string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		if u, err := url.Parse(strings.TrimSpace(o)); err == nil && u.Host != "" {
			allow[u.Scheme+"://"+u.Host] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				WriteError(r.Context(), w, NewError(CodeForbidden, "拒绝跨站请求"))
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if u, err := url.Parse(origin); err == nil {
				if strings.EqualFold(u.Host, r.Host) {
					next.ServeHTTP(w, r)
					return
				}
			}
			if _, ok := allow[origin]; ok {
				next.ServeHTTP(w, r)
				return
			}
			WriteError(r.Context(), w, NewError(CodeForbidden, "拒绝跨站请求"))
		})
	}
}
