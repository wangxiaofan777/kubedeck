package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/cluster"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

// ClusterAdmin 是集群登记与凭据管理的最小依赖。
type ClusterAdmin interface {
	Create(ctx context.Context, req cluster.CreateRequest) (domain.Cluster, error)
	Rotate(ctx context.Context, name string, in kube.IngestInput) (domain.Cluster, error)
	UpdateMeta(ctx context.Context, name, displayName string, labels map[string]string) (domain.Cluster, error)
	Delete(ctx context.Context, name string) error
	CredentialMeta(ctx context.Context, name string) (domain.Credential, error)
	Info(ctx context.Context, name string) (kube.ClusterInfo, bool)
}

type clusterCredentialRequest struct {
	Kubeconfig            string `json:"kubeconfig"`
	Server                string `json:"server"`
	Token                 string `json:"token"`
	CAData                string `json:"caData"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify"`
}

func (r clusterCredentialRequest) ingest() kube.IngestInput {
	return kube.IngestInput{
		Kubeconfig: r.Kubeconfig, Server: r.Server, Token: r.Token,
		CAData: r.CAData, InsecureSkipTLSVerify: r.InsecureSkipTLSVerify,
	}
}

// createCluster 接入集群；仅平台管理员可调用。
func (h *handlers) createCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string            `json:"name"`
		DisplayName string            `json:"displayName"`
		Labels      map[string]string `json:"labels"`
		clusterCredentialRequest
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	createdBy := id.UserID
	c, err := h.clusterAdmin.Create(r.Context(), cluster.CreateRequest{
		Name:        body.Name,
		DisplayName: body.DisplayName,
		Labels:      body.Labels,
		Credential:  body.ingest(),
		CreatedBy:   &createdBy,
	})
	if err != nil {
		writeClusterError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// getCluster 返回集群详情与脱敏凭据信息。
func (h *handlers) getCluster(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	name := chi.URLParam(r, "cluster")
	if allowed, err := h.clusterAccess(r.Context(), id, name, "*"); err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	} else if !allowed {
		WriteError(r.Context(), w, NewError(CodeForbidden, "无权访问该集群"))
		return
	}
	info, ok := h.clusterAdmin.Info(r.Context(), name)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return
	}
	resp := map[string]any{"health": info}
	if id.PlatformAdmin {
		if cred, err := h.clusterAdmin.CredentialMeta(r.Context(), name); err == nil {
			resp["credential"] = maskCredential(cred)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// patchCluster 修改展示名与标签。
func (h *handlers) patchCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName string            `json:"displayName"`
		Labels      map[string]string `json:"labels"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	c, err := h.clusterAdmin.UpdateMeta(r.Context(), chi.URLParam(r, "cluster"), body.DisplayName, body.Labels)
	if err != nil {
		writeClusterError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// deleteCluster 要求输入集群名二次确认；只删登记，不触碰目标集群。
func (h *handlers) deleteCluster(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "cluster")
	var body struct {
		ConfirmName string `json:"confirmName"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ConfirmName != name {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "确认名称与集群名不一致"))
		return
	}
	if err := h.clusterAdmin.Delete(r.Context(), name); err != nil {
		writeClusterError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rotateCredential 用新凭据覆盖旧凭据；探测失败时不修改现有凭据。
func (h *handlers) rotateCredential(w http.ResponseWriter, r *http.Request) {
	var body clusterCredentialRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	c, err := h.clusterAdmin.Rotate(r.Context(), chi.URLParam(r, "cluster"), body.ingest())
	if err != nil {
		writeClusterError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// getCredential 返回凭据元数据，绝不返回 token 或证书私钥。
func (h *handlers) getCredential(w http.ResponseWriter, r *http.Request) {
	cred, err := h.clusterAdmin.CredentialMeta(r.Context(), chi.URLParam(r, "cluster"))
	if err != nil {
		writeClusterError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, maskCredential(cred))
}

// getClusterHealth 返回最近一次健康探测结果。
func (h *handlers) getClusterHealth(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.IdentityFrom(r.Context())
	if !ok {
		WriteError(r.Context(), w, NewError(CodeUnauthenticated, "登录状态无效或已过期"))
		return
	}
	name := chi.URLParam(r, "cluster")
	if allowed, err := h.clusterAccess(r.Context(), id, name, "*"); err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	} else if !allowed {
		WriteError(r.Context(), w, NewError(CodeForbidden, "无权访问该集群"))
		return
	}
	info, ok := h.clusterAdmin.Info(r.Context(), name)
	if !ok {
		WriteError(r.Context(), w, NewError(CodeNotFound, "集群不存在"))
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func maskCredential(c domain.Credential) map[string]any {
	return map[string]any{
		"authType":    c.AuthType,
		"fingerprint": c.Fingerprint,
		"keyVersion":  c.KeyVersion,
		"rotatedAt":   c.RotatedAt,
		"expiresAt":   c.ExpiresAt,
	}
}

func writeClusterError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		e := NewError(CodeInvalidArgument, err.Error())
		WriteError(ctx, w, e)
	case errors.Is(err, cluster.ErrNotSupported):
		WriteError(ctx, w, NewError(CodeInvalidArgument, err.Error()))
	case errors.Is(err, cluster.ErrUnreachable):
		e := NewError(CodeClusterUnreachable, err.Error())
		WriteError(ctx, w, e)
	case errors.Is(err, cluster.ErrNoMasterKey):
		WriteError(ctx, w, NewError(CodeClusterDegraded, "平台未配置主密钥，无法保存集群凭据"))
	case errors.Is(err, domain.ErrConflict):
		WriteError(ctx, w, NewError(CodeConflict, "集群名称已存在"))
	case errors.Is(err, domain.ErrNotFound):
		WriteError(ctx, w, NewError(CodeNotFound, "集群不存在"))
	default:
		WriteError(ctx, w, NewError(CodeInternal, "集群操作失败"))
	}
}
