package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/kubedeck/kubedeck/internal/store"
)

const (
	defaultAuditStatsWindow = 7 * 24 * time.Hour
	maxAuditStatsWindow     = 90 * 24 * time.Hour
)

// auditStats 返回审计聚合统计；只输出计数与分组键。
func (h *handlers) auditStats(w http.ResponseWriter, r *http.Request) {
	if h.auditStore == nil {
		WriteError(r.Context(), w, NewError(CodeClusterDegraded, "审计存储未配置"))
		return
	}
	filter, err := parseAuditStatsFilter(r, time.Now().UTC())
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}
	stats, err := h.auditStore.AuditStats(r.Context(), filter)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "审计统计失败"))
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// parseAuditStatsFilter 解析时间范围：默认最近 7 天，跨度上限 90 天。
func parseAuditStatsFilter(r *http.Request, now time.Time) (store.AuditStatsFilter, error) {
	q := r.URL.Query()
	to := now
	if raw := q.Get("to"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return store.AuditStatsFilter{}, errors.New("to 必须是 RFC3339 时间")
		}
		to = parsed.UTC()
	}
	from := to.Add(-defaultAuditStatsWindow)
	if raw := q.Get("from"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return store.AuditStatsFilter{}, errors.New("from 必须是 RFC3339 时间")
		}
		from = parsed.UTC()
	}
	if !from.Before(to) {
		return store.AuditStatsFilter{}, errors.New("from 必须早于 to")
	}
	if to.Sub(from) > maxAuditStatsWindow {
		return store.AuditStatsFilter{}, errors.New("统计范围不能超过 90 天")
	}
	return store.AuditStatsFilter{From: from, To: to, ClusterName: q.Get("cluster")}, nil
}
