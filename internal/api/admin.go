package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
)

// listUsers 供权限管理界面搜索用户。
func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.adminStore.SearchUsers(r.Context(), r.URL.Query().Get("q"), 50)
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// listRoles 返回平台预置角色。
func (h *handlers) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.adminStore.Roles(r.Context())
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

// listAdminClusters 返回带主键的集群列表，供绑定表单使用。
func (h *handlers) listAdminClusters(w http.ResponseWriter, r *http.Request) {
	clusters, err := h.adminStore.Clusters(r.Context())
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"clusters": clusters})
}

// listRoleBindings 返回绑定；?userId= 可过滤单个用户。
func (h *handlers) listRoleBindings(w http.ResponseWriter, r *http.Request) {
	all, err := h.adminStore.AllRoleBindings(r.Context())
	if err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	userID, _ := strconv.ParseInt(r.URL.Query().Get("userId"), 10, 64)
	if userID == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"bindings": all})
		return
	}
	filtered := make([]domain.RoleBinding, 0)
	for _, b := range all {
		if b.UserID == userID {
			filtered = append(filtered, b)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": filtered})
}

// putUserAccess 整体覆盖用户在某个集群上的角色绑定，60 秒内生效。
func (h *handlers) putUserAccess(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID    int64                     `json:"userId"`
		ClusterID int64                     `json:"clusterId"`
		Bindings  []domain.RoleBindingInput `json:"bindings"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.UserID <= 0 || body.ClusterID <= 0 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "userId 与 clusterId 必须为正整数"))
		return
	}
	if _, err := h.adminStore.ClusterByID(r.Context(), body.ClusterID); err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	grantedBy := id.UserID
	if err := h.adminStore.SetClusterAccess(r.Context(), body.UserID, body.ClusterID, body.Bindings, &grantedBy); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(r.Context(), w, NewError(CodeInvalidArgument, "角色或用户不存在"))
			return
		}
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteRoleBinding 回收单条绑定。
func (h *handlers) deleteRoleBinding(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "绑定 ID 不合法"))
		return
	}
	if err := h.adminStore.DeleteRoleBinding(r.Context(), id); err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setUserStatus 启用或禁用用户；禁用时立即终止会话。
func (h *handlers) setUserStatus(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "用户 ID 不合法"))
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Status != "active" && body.Status != "disabled" {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "status 必须是 active 或 disabled"))
		return
	}
	if err := h.adminStore.SetUserStatus(r.Context(), userID, body.Status); err != nil {
		WriteError(r.Context(), w, FromPlatformError(err))
		return
	}
	if body.Status == "disabled" && h.authSvc != nil {
		h.authSvc.LogoutUser(userID)
	}
	w.WriteHeader(http.StatusNoContent)
}
