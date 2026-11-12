package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const auditColumns = `id, occurred_at, request_id, actor_id, actor_subject, actor_email,
	cluster_id, cluster_name, verb, api_group, resource, namespace, name,
	result, result_code, latency_ms, COALESCE(host(client_ip), ''), user_agent,
	body_hash, detail, prev_hash, hash`

// AuditFilter 是审计查询条件；零值表示不限。
type AuditFilter struct {
	From         *time.Time
	To           *time.Time
	ActorSubject string
	ClusterName  string
	Resource     string
	Result       string
	Limit        int
	Offset       int
}

// QueryAudit 按时间倒序查询审计记录。
func (s *Store) QueryAudit(ctx context.Context, f AuditFilter) ([]domain.AuditEntry, error) {
	var (
		where []string
		args  []any
	)
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.From != nil {
		add("occurred_at >= $%d", f.From.UTC())
	}
	if f.To != nil {
		add("occurred_at < $%d", f.To.UTC())
	}
	if f.ActorSubject != "" {
		add("actor_subject = $%d", f.ActorSubject)
	}
	if f.ClusterName != "" {
		add("cluster_name = $%d", f.ClusterName)
	}
	if f.Resource != "" {
		add("resource = $%d", f.Resource)
	}
	if f.Result != "" {
		add("result = $%d", f.Result)
	}

	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	limit, offset := pageArgs(f.Limit, f.Offset)
	args = append(args, limit, offset)

	sql := fmt.Sprintf(`SELECT %s FROM audit_logs%s ORDER BY occurred_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		auditColumns, clause, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
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

func scanAuditEntry(row pgx.Row) (domain.AuditEntry, error) {
	var (
		e      domain.AuditEntry
		detail []byte
	)
	err := row.Scan(&e.ID, &e.OccurredAt, &e.RequestID, &e.ActorID, &e.ActorSubject, &e.ActorEmail,
		&e.ClusterID, &e.ClusterName, &e.Verb, &e.APIGroup, &e.Resource, &e.Namespace, &e.ObjectName,
		&e.Result, &e.ResultCode, &e.LatencyMS, &e.ClientIP, &e.UserAgent,
		&e.BodyHash, &detail, &e.PrevHash, &e.Hash)
	if errors.Is(err, pgx.ErrNoRows) {
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
