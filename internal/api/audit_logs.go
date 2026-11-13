package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

// AuditStore 是审计查询与校验所需的最小仓储能力。
type AuditStore interface {
	QueryAudit(ctx context.Context, filter store.AuditFilter) ([]domain.AuditEntry, error)
	CountAudit(ctx context.Context, filter store.AuditFilter) (int, error)
	VerifyAuditChain(ctx context.Context) (store.ChainStatus, error)
	RetentionDays(ctx context.Context) (int, error)
	AuditStats(ctx context.Context, filter store.AuditStatsFilter) (store.AuditStats, error)
}

// listAuditLogs 按过滤条件分页返回审计记录。
func (h *handlers) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	if h.auditStore == nil {
		WriteError(r.Context(), w, NewError(CodeClusterDegraded, "审计存储未配置"))
		return
	}
	filter, err := parseAuditFilter(r)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}
	entries, err := h.auditStore.QueryAudit(r.Context(), filter)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "审计查询失败"))
		return
	}
	total, err := h.auditStore.CountAudit(r.Context(), filter)
	if err != nil {
		total = len(entries)
	}
	resp := map[string]any{
		"items":  entries,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	}
	if warning := h.retentionWarning(r.Context(), filter.From); warning != "" {
		resp["retentionWarning"] = warning
	}
	writeJSON(w, http.StatusOK, resp)
}

// verifyAuditChain 重算哈希链并返回第一个断裂点。
func (h *handlers) verifyAuditChain(w http.ResponseWriter, r *http.Request) {
	if h.auditStore == nil {
		WriteError(r.Context(), w, NewError(CodeClusterDegraded, "审计存储未配置"))
		return
	}
	status, err := h.auditStore.VerifyAuditChain(r.Context())
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "审计链校验失败"))
		return
	}
	resp := map[string]any{
		"checked": status.Checked,
		"intact":  status.BrokenID == 0,
	}
	if status.BrokenID != 0 {
		resp["brokenId"] = status.BrokenID
		resp["brokenKind"] = status.BrokenKind
	}
	writeJSON(w, http.StatusOK, resp)
}

// exportAuditLogs 导出查询结果（CSV/JSON），并记录一次导出审计。
func (h *handlers) exportAuditLogs(w http.ResponseWriter, r *http.Request) {
	if h.auditStore == nil {
		WriteError(r.Context(), w, NewError(CodeClusterDegraded, "审计存储未配置"))
		return
	}
	filter, err := parseAuditFilter(r)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, err.Error()))
		return
	}
	if filter.Limit <= 0 || filter.Limit > 5000 {
		filter.Limit = 5000
	}
	filter.Offset = 0
	entries, err := h.auditStore.QueryAudit(r.Context(), filter)
	if err != nil {
		WriteError(r.Context(), w, NewError(CodeInternal, "审计导出失败"))
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "csv"
	}
	now := time.Now().UTC().Format("20060102-150405")
	switch format {
	case "json":
		body, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			WriteError(r.Context(), w, NewError(CodeInternal, "审计导出失败"))
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="kubedeck-audit-%s.json"`, now))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case "csv":
		var buf bytes.Buffer
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{
			"id", "occurredAt", "requestId", "actor", "actorEmail", "cluster", "verb",
			"apiGroup", "resource", "namespace", "name", "result", "resultCode",
			"latencyMs", "clientIp", "bodyHash",
		})
		for _, e := range entries {
			_ = cw.Write([]string{
				strconv.FormatInt(e.ID, 10), e.OccurredAt.UTC().Format(time.RFC3339Nano),
				e.RequestID, e.ActorSubject, e.ActorEmail, e.ClusterName, e.Verb,
				e.APIGroup, e.Resource, e.Namespace, e.ObjectName, e.Result,
				strconv.Itoa(e.ResultCode), strconv.Itoa(e.LatencyMS), e.ClientIP, e.BodyHash,
			})
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			WriteError(r.Context(), w, NewError(CodeInternal, "审计导出失败"))
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="kubedeck-audit-%s.csv"`, now))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	default:
		WriteError(r.Context(), w, NewError(CodeInvalidArgument, "format 必须是 csv 或 json"))
		return
	}

	h.recordExportAudit(r, format, len(entries), filter)
}

func (h *handlers) recordExportAudit(r *http.Request, format string, count int, filter store.AuditFilter) {
	if h.audit == nil {
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	entry := domain.AuditEntry{
		OccurredAt:   time.Now().UTC(),
		RequestID:    RequestIDFrom(r.Context()),
		ActorSubject: id.Subject,
		ActorEmail:   id.Email,
		Verb:         "export",
		Resource:     "audit_logs",
		Result:       "success",
		ResultCode:   http.StatusOK,
		ClientIP:     clientIP(r),
		UserAgent:    r.UserAgent(),
		Detail: map[string]any{
			"format": format,
			"count":  count,
		},
	}
	if id.UserID > 0 {
		uid := id.UserID
		entry.ActorID = &uid
	}
	if filter.From != nil {
		entry.Detail["from"] = filter.From.UTC()
	}
	if filter.To != nil {
		entry.Detail["to"] = filter.To.UTC()
	}
	h.audit.Record(entry)
}

func parseAuditFilter(r *http.Request) (store.AuditFilter, error) {
	q := r.URL.Query()
	filter := store.AuditFilter{
		ActorSubject: q.Get("actor"),
		ClusterName:  q.Get("cluster"),
		Resource:     q.Get("resource"),
		Result:       q.Get("result"),
	}
	if raw := q.Get("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, errors.New("from 必须是 RFC3339 时间")
		}
		filter.From = &t
	}
	if raw := q.Get("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, errors.New("to 必须是 RFC3339 时间")
		}
		filter.To = &t
	}
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			return filter, errors.New("limit 必须在 1-500 之间")
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return filter, errors.New("offset 必须是非负整数")
		}
		filter.Offset = n
	}
	filter.Limit = limit
	return filter, nil
}

// retentionWarning 在查询起点早于保留期时提示数据可能已不可用。
func (h *handlers) retentionWarning(ctx context.Context, from *time.Time) string {
	if from == nil || h.auditStore == nil {
		return ""
	}
	days, err := h.auditStore.RetentionDays(ctx)
	if err != nil || days <= 0 {
		return ""
	}
	if from.Before(time.Now().Add(-time.Duration(days) * 24 * time.Hour)) {
		return "查询起点早于审计保留期，部分数据可能已归档或不可用"
	}
	return ""
}
