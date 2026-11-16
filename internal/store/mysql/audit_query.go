package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

const auditColumns = `id, occurred_at, request_id, actor_id, actor_subject, actor_email,
	cluster_id, cluster_name, verb, api_group, resource, namespace, name,
	result, result_code, latency_ms, COALESCE(client_ip, ''), user_agent,
	body_hash, detail, prev_hash, hash`

// auditWhere 构造过滤条件；参数按出现顺序追加。
func auditWhere(f store.AuditFilter) (string, []any) {
	var (
		where []string
		args  []any
	)
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, cond)
	}
	if f.From != nil {
		add("occurred_at >= ?", f.From.UTC())
	}
	if f.To != nil {
		add("occurred_at < ?", f.To.UTC())
	}
	if f.ActorSubject != "" {
		add("actor_subject = ?", f.ActorSubject)
	}
	if f.ClusterName != "" {
		add("cluster_name = ?", f.ClusterName)
	}
	if f.Resource != "" {
		add("resource = ?", f.Resource)
	}
	if f.Result != "" {
		add("result = ?", f.Result)
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// CountAudit 返回满足条件的记录数。
func (s *Store) CountAudit(ctx context.Context, f store.AuditFilter) (int, error) {
	clause, args := auditWhere(f)
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs`+clause, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count audit logs: %w", err)
	}
	return n, nil
}

// QueryAudit 按时间倒序查询审计记录。
func (s *Store) QueryAudit(ctx context.Context, f store.AuditFilter) ([]domain.AuditEntry, error) {
	clause, args := auditWhere(f)
	limit, offset := store.AuditPageArgs(f.Limit, f.Offset)
	args = append(args, limit, offset)

	query := `SELECT ` + auditColumns + ` FROM audit_logs` + clause +
		` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query audit logs: %w", err)
	}
	defer rows.Close()

	entries := make([]domain.AuditEntry, 0, limit)
	for rows.Next() {
		e, err := scanAuditEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, mapError(rows.Err())
}

func scanAuditEntry(row rowScanner) (domain.AuditEntry, error) {
	var (
		e      domain.AuditEntry
		detail []byte
	)
	err := row.Scan(&e.ID, &e.OccurredAt, &e.RequestID, &e.ActorID, &e.ActorSubject, &e.ActorEmail,
		&e.ClusterID, &e.ClusterName, &e.Verb, &e.APIGroup, &e.Resource, &e.Namespace, &e.ObjectName,
		&e.Result, &e.ResultCode, &e.LatencyMS, &e.ClientIP, &e.UserAgent,
		&e.BodyHash, &detail, &e.PrevHash, &e.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.AuditEntry{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AuditEntry{}, fmt.Errorf("store: scan audit log: %w", err)
	}
	if len(detail) > 0 {
		if err := json.Unmarshal(detail, &e.Detail); err != nil {
			return domain.AuditEntry{}, fmt.Errorf("store: decode audit detail: %w", err)
		}
	}
	return e, nil
}
