package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCClaims 是平台关心的身份字段。
type OIDCClaims struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	Name          string   `json:"name"`
	Username      string   `json:"preferred_username"`
	Groups        []string `json:"groups"`
	EmailVerified *bool    `json:"email_verified"`
}

const (
	maxOIDCGroups   = 64
	maxOIDCGroupLen = 256
)

// Provider 封装 OIDC discovery、授权码交换与 ID Token 校验。
type Provider struct {
	issuer              string
	clientID            string
	oauth               *oauth2.Config
	verifier            *oidc.IDTokenVerifier
	raw                 *oidc.Provider
	allowUnverifiedMail bool
	logger              *slog.Logger
}

// OIDCConfig 是构造 Provider 所需配置。
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	// AllowUnverifiedEmail 允许 ID Token 中 email_verified 缺失或为 false 时继续登录。
	// 默认 false：IdP 明确返回未验证邮箱时拒绝建立会话。
	AllowUnverifiedEmail bool
}

// NewProvider 执行 OIDC discovery。IdP 不可达时启动失败，避免带病运行。
func NewProvider(ctx context.Context, cfg OIDCConfig) (*Provider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, errors.New("auth: oidc issuer, client id and redirect url are required")
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	}
	raw, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover oidc provider %q: %w", cfg.Issuer, err)
	}
	return &Provider{
		issuer:              cfg.Issuer,
		clientID:            cfg.ClientID,
		raw:                 raw,
		verifier:            raw.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		allowUnverifiedMail: cfg.AllowUnverifiedEmail,
		logger:              slog.Default(),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     raw.Endpoint(),
			Scopes:       scopes,
		},
	}, nil
}

// AuthCodeURL 生成授权码 + PKCE 的跳转地址。
func (p *Provider) AuthCodeURL(state, verifier, nonce string) string {
	return p.oauth.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

// Exchange 用授权码换取身份。PKCE verifier 与 nonce 必须与发起登录时一致。
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, nonce string) (OIDCClaims, string, error) {
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return OIDCClaims{}, "", fmt.Errorf("auth: exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return OIDCClaims{}, "", errors.New("auth: idp response has no id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return OIDCClaims{}, "", fmt.Errorf("auth: verify id_token: %w", err)
	}
	if idToken.Nonce != nonce {
		return OIDCClaims{}, "", errors.New("auth: id_token nonce mismatch")
	}

	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		return OIDCClaims{}, "", fmt.Errorf("auth: decode id_token claims: %w", err)
	}
	if claims.Subject == "" {
		return OIDCClaims{}, "", errors.New("auth: id_token has empty subject")
	}
	if err := p.validateEmail(claims); err != nil {
		return OIDCClaims{}, "", err
	}
	claims.Groups = p.cleanGroups(claims.Groups)
	if claims.Email == "" {
		// 部分 IdP 只在 userinfo 返回 email；缺失时退回 preferred_username。
		claims.Email = claims.Username
	}
	return claims, token.AccessToken, nil
}

// validateEmail 在默认严格模式下拒绝未验证或缺少验证状态的邮箱。
// 没有 email 时身份退回 subject，不适用邮箱验证。
func (p *Provider) validateEmail(claims OIDCClaims) error {
	if p.allowUnverifiedMail || claims.Email == "" {
		return nil
	}
	if claims.EmailVerified == nil {
		return errors.New("auth: id_token email is missing email_verified")
	}
	if !*claims.EmailVerified {
		return errors.New("auth: id_token email is not verified")
	}
	return nil
}

// cleanGroups 去掉异常声明，避免把未经限制的组直接放大到每个集群请求。
func (p *Provider) cleanGroups(groups []string) []string {
	cleaned, dropped := normalizeGroups(groups)
	if dropped > 0 {
		p.logger.Warn("oidc group claim truncated", "dropped", dropped, "kept", len(cleaned))
	}
	return cleaned
}

func normalizeGroups(groups []string) ([]string, int) {
	capacity := len(groups)
	if capacity > maxOIDCGroups {
		capacity = maxOIDCGroups
	}
	out := make([]string, 0, capacity)
	seen := make(map[string]struct{}, capacity)
	dropped := 0
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if len(group) > maxOIDCGroupLen {
			dropped++
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		if len(out) >= maxOIDCGroups {
			dropped++
			continue
		}
		seen[group] = struct{}{}
		out = append(out, group)
	}
	return out, dropped
}

// UserInfo 用于会话周期校验；IdP 侧禁用用户后返回 4xx。
func (p *Provider) UserInfo(ctx context.Context, accessToken string) (OIDCClaims, error) {
	info, err := p.raw.UserInfo(ctx, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken}))
	if err != nil {
		return OIDCClaims{}, err
	}
	claims := OIDCClaims{
		Subject: info.Subject,
		Email:   info.Email,
	}
	if err := info.Claims(&claims); err != nil {
		return OIDCClaims{}, fmt.Errorf("auth: decode userinfo: %w", err)
	}
	claims.Groups = p.cleanGroups(claims.Groups)
	if claims.Email == "" {
		claims.Email = claims.Username
	}
	return claims, nil
}

// Issuer 返回配置的 IdP 地址。
func (p *Provider) Issuer() string { return p.issuer }

// IsRevoked 判断 userinfo 错误是否代表身份已失效（4xx），而非 IdP 暂时不可用。
func IsRevoked(err error) bool {
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		return retrieve.Response != nil && retrieve.Response.StatusCode >= 400 && retrieve.Response.StatusCode < 500
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid_token") || strings.Contains(msg, "unauthorized")
}
