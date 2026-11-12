// Package auth 实现 OIDC 登录、平台会话与身份上下文。
package auth

import (
	"context"

	"github.com/kubedeck/kubedeck/internal/kube"
)

// Identity 是当前请求的登录用户。KubeUser 是传给 apiserver 的 Impersonate-User，
// 优先使用邮箱（企业 RBAC 通常按邮箱绑定），缺失时退回 subject。
type Identity struct {
	UserID        int64    `json:"userId"`
	Subject       string   `json:"subject"`
	KubeUser      string   `json:"-"`
	Email         string   `json:"email"`
	DisplayName   string   `json:"displayName"`
	Groups        []string `json:"groups,omitempty"`
	PlatformAdmin bool     `json:"isPlatformAdmin"`
	Dev           bool     `json:"dev,omitempty"`
}

// UserRef 返回集群访问所需的身份。KubeUser 为空表示开发模式下直接使用
// kubeconfig 自身身份（不发起 Impersonation）。
func (i Identity) UserRef() kube.UserRef {
	return kube.UserRef{Name: i.KubeUser, Groups: i.Groups}
}

type identityKey struct{}

// WithIdentity 把登录身份写入 context。
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFrom 读取登录身份；未认证时 ok 为 false。
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey{}).(Identity)
	return id, ok
}

// Session 是服务端保存的登录会话。访问令牌只留在服务端内存，不写入 Cookie。
type Session struct {
	ID            string
	Identity      Identity
	AccessToken   string
	IssuedAt      int64
	ExpiresAt     int64
	LastValidated int64
}
