package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
)

var (
	// ErrInvalidState 表示 OIDC state 缺失、过期或与 Cookie 不匹配。
	ErrInvalidState = errors.New("auth: invalid or expired login state")
	// ErrNoSession 表示会话不存在或已过期。
	ErrNoSession = errors.New("auth: session not found")
	// ErrLoginUnavailable 表示登录依赖不可用（IdP 或数据库）。
	ErrLoginUnavailable = errors.New("auth: login unavailable")
)

// UserStore 是认证需要的用户仓储能力。
type UserStore interface {
	UpsertUser(ctx context.Context, u domain.User) (domain.User, error)
	SetUserStatus(ctx context.Context, id int64, status string) error
}

// Service 编排登录、会话与 IdP 撤销检测。
type Service struct {
	provider *Provider
	users    UserStore
	sessions *MemorySessions
	dev      *Identity
	logger   *slog.Logger

	mu     sync.Mutex
	states map[string]loginState
	now    func() time.Time
}

type loginState struct {
	verifier  string
	nonce     string
	returnTo  string
	expiresAt time.Time
}

// Config 是 Service 的构造参数。Provider 为空表示开发模式（仅允许 dev 身份）。
type Config struct {
	Provider *Provider
	Users    UserStore
	Sessions *MemorySessions
	Dev      *Identity
	Logger   *slog.Logger
}

// NewService 创建认证服务。
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		provider: cfg.Provider,
		users:    cfg.Users,
		sessions: cfg.Sessions,
		dev:      cfg.Dev,
		logger:   logger,
		states:   make(map[string]loginState),
		now:      time.Now,
	}
}

// DevMode 表示是否启用了本地开发身份。
func (s *Service) DevMode() bool { return s.dev != nil }

// DevIdentity 返回开发身份副本。
func (s *Service) DevIdentity() (Identity, bool) {
	if s.dev == nil {
		return Identity{}, false
	}
	return *s.dev, true
}

// Sessions 暴露会话存储给鉴权中间件。
func (s *Service) Sessions() *MemorySessions { return s.sessions }

// StartLogin 生成 OIDC 授权地址，并记住本次登录的 PKCE verifier 与 nonce。
func (s *Service) StartLogin(returnTo string) (authURL, state string, err error) {
	if s.provider == nil {
		return "", "", ErrLoginUnavailable
	}
	state, err = randomToken()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomToken()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", err
	}

	s.mu.Lock()
	s.pruneStatesLocked()
	s.states[state] = loginState{
		verifier:  verifier,
		nonce:     nonce,
		returnTo:  safeReturnTo(returnTo),
		expiresAt: s.now().Add(10 * time.Minute),
	}
	s.mu.Unlock()

	return s.provider.AuthCodeURL(state, verifier, nonce), state, nil
}

// FinishLogin 校验 state 与 PKCE，换取身份并建立会话。
// cookieState 是发起登录时写入浏览器 Cookie 的值，必须与回调参数一致。
func (s *Service) FinishLogin(ctx context.Context, state, cookieState, code string) (Session, string, error) {
	if s.provider == nil || s.users == nil {
		return Session{}, "", ErrLoginUnavailable
	}
	if state == "" || state != cookieState || code == "" {
		return Session{}, "", ErrInvalidState
	}

	s.mu.Lock()
	st, ok := s.states[state]
	delete(s.states, state)
	s.mu.Unlock()
	if !ok || s.now().After(st.expiresAt) {
		return Session{}, "", ErrInvalidState
	}

	claims, accessToken, err := s.provider.Exchange(ctx, code, st.verifier, st.nonce)
	if err != nil {
		return Session{}, "", err
	}

	user, err := s.users.UpsertUser(ctx, domain.User{
		Subject:     claims.Subject,
		Email:       claims.Email,
		DisplayName: displayName(claims),
		Source:      "oidc",
	})
	if err != nil {
		return Session{}, "", fmt.Errorf("auth: upsert user: %w", err)
	}
	if user.Status != "active" {
		return Session{}, "", fmt.Errorf("%w: user is disabled", ErrLoginUnavailable)
	}

	session, err := s.sessions.Create(identityFromUser(user, claims.Groups, false), accessToken)
	if err != nil {
		return Session{}, "", err
	}
	return session, st.returnTo, nil
}

// NewDevSession 为开发模式建立会话（不经过 IdP）。
func (s *Service) NewDevSession() (Session, error) {
	if s.dev == nil {
		return Session{}, ErrLoginUnavailable
	}
	return s.sessions.Create(*s.dev, "")
}

// Logout 立即失效会话。
func (s *Service) Logout(sid string) {
	s.sessions.Delete(sid)
}

// LogoutUser 立即失效某个用户的全部会话。
func (s *Service) LogoutUser(userID int64) {
	s.sessions.DeleteUser(userID)
}

// Session 读取会话。
func (s *Service) Session(sid string) (Session, bool) {
	if sid == "" {
		return Session{}, false
	}
	return s.sessions.Get(sid)
}

// DisableUser 禁用用户并立即终止其全部会话。
func (s *Service) DisableUser(ctx context.Context, userID int64) error {
	if s.users == nil {
		return ErrLoginUnavailable
	}
	if err := s.users.SetUserStatus(ctx, userID, "disabled"); err != nil {
		return err
	}
	s.sessions.DeleteUser(userID)
	return nil
}

// RevalidateIdP 周期性校验在线会话。IdP 明确拒绝时终止会话；IdP 不可用时保留会话，
// 满足「IdP 故障不能让已登录用户被登出」的约束。
func (s *Service) RevalidateIdP(ctx context.Context) {
	if s.provider == nil {
		return
	}
	now := s.now().UTC()
	for _, sess := range s.sessions.List() {
		if sess.AccessToken == "" {
			continue
		}
		if now.Unix()-sess.LastValidated < 60 {
			continue
		}
		claims, err := s.provider.UserInfo(ctx, sess.AccessToken)
		if err != nil {
			if IsRevoked(err) {
				s.sessions.DeleteUser(sess.Identity.UserID)
				s.logger.Warn("session revoked by idp",
					"user", sess.Identity.Subject, "requestId", "")
			}
			continue
		}
		if claims.Subject != "" && claims.Subject != sess.Identity.Subject {
			s.sessions.Delete(sess.ID)
			continue
		}
		s.sessions.MarkValidated(sess.ID, now)
	}
}

func (s *Service) pruneStatesLocked() {
	now := s.now()
	for state, st := range s.states {
		if now.After(st.expiresAt) {
			delete(s.states, state)
		}
	}
}

func identityFromUser(u domain.User, groups []string, dev bool) Identity {
	kubeUser := u.Email
	if kubeUser == "" {
		kubeUser = u.Subject
	}
	return Identity{
		UserID:        u.ID,
		Subject:       u.Subject,
		KubeUser:      kubeUser,
		Email:         u.Email,
		DisplayName:   u.DisplayName,
		Groups:        groups,
		PlatformAdmin: u.IsPlatformAdmin,
		Dev:           dev,
	}
}

func displayName(c OIDCClaims) string {
	if c.Name != "" {
		return c.Name
	}
	if c.Username != "" {
		return c.Username
	}
	return c.Email
}

func safeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
