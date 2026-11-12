package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// auditChainLockKey 串行化哈希链追加，避免并发写入产生分叉链。
const auditChainLockKey int64 = 0x6175646974636861 // "auditcha"

const auditColumns = `id, occurred_at, request_id, actor_id, actor_subject, actor_email,
	cluster_id, cluster_name, verb, api_group, resource, namespace, name,
	result, result_code, latency_ms, COALESCE(host(client_ip), ''), user_agent,
	body_hash, detail, prev_hash, hash`

const auditHashArgs = `$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
	$14, $15, NULLIF($16, '')::inet, $17, $18, $19, $20`

// AppendAudit 追加一条审计记录，并在同一事务内把它挂到哈希链尾部。
// 审计指向 apiserver 的结果，不改变业务返回值；调用方负责失败时的补偿队列。
func (s *Store) AppendAudit(ctx context.Context, e domain.AuditEntry) (domain.AuditRecord, error) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	} else {
		e.OccurredAt = e.OccurredAt.UTC()
	}
	if e.Result == "" {
		e.Result = "success"
	}
	if e.Detail == nil {
		e.Detail = map[string]any{}
	}
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return domain.AuditRecord{}, fmt.Errorf("store: encode audit detail: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.AuditRecord{}, fmt.Errorf("store: begin audit tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditChainLockKey); err != nil {
		return domain.AuditRecord{}, fmt.Errorf("store: lock audit chain: %w", err)
	}

	var prevHash string
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&prevHash)
	if errors.Is(err, pgx.ErrNoRows) {
		prevHash = ""
	} else if err != nil {
		return domain.AuditRecord{}, fmt.Errorf("store: read audit chain tail: %w", err)
	}
	e.PrevHash = prevHash

	var rec domain.AuditRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO audit_logs (
			occurred_at, request_id, actor_id, actor_subject, actor_email,
			cluster_id, cluster_name, verb, api_group, resource, namespace, name,
			result, result_code, latency_ms, client_ip, user_agent, body_hash, detail, prev_hash, hash
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			NULLIF($16, '')::inet, $17, $18, $19, $20,
			audit_log_hash(`+auditHashArgs+`)
		)
		RETURNING id, occurred_at, prev_hash, hash`,
		e.OccurredAt, e.RequestID, e.ActorID, e.ActorSubject, e.ActorEmail,
		e.ClusterID, e.ClusterName, e.Verb, e.APIGroup, e.Resource, e.Namespace, e.ObjectName,
		e.Result, e.ResultCode, e.LatencyMS, e.ClientIP, e.UserAgent, e.BodyHash,
		detail, e.PrevHash,
	).Scan(&rec.ID, &rec.OccurredAt, &rec.PrevHash, &rec.Hash)
	if err != nil {
		return domain.AuditRecord{}, fmt.Errorf("store: insert audit log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.AuditRecord{}, fmt.Errorf("store: commit audit log: %w", err)
	}
	return rec, nil
}

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

// ChainStatus 是哈希链校验结果。
type ChainStatus struct {
	Checked    int    `json:"checked"`
	BrokenID   int64  `json:"brokenId,omitempty"`
	BrokenKind string `json:"brokenKind,omitempty"`
}

// VerifyAuditChain 逐条重算哈希并检查前后衔接，返回第一个断裂点（如有）。
func (s *Store) VerifyAuditChain(ctx context.Context) (ChainStatus, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, hash, prev_hash,
		       audit_log_hash(occurred_at, request_id, actor_id, actor_subject, actor_email,
		                      cluster_id, cluster_name, verb, api_group, resource, namespace, name,
		                      result, result_code, latency_ms, client_ip, user_agent,
		                      body_hash, detail, prev_hash) AS computed
		FROM audit_logs ORDER BY id`)
	if err != nil {
		return ChainStatus{}, fmt.Errorf("store: verify audit chain: %w", err)
	}
	defer rows.Close()

	var (
		status ChainStatus
		prev   string
	)
	for rows.Next() {
		var (
			id       int64
			hash     string
			prevHash string
			computed string
		)
		if err := rows.Scan(&id, &hash, &prevHash, &computed); err != nil {
			return ChainStatus{}, fmt.Errorf("store: scan audit chain row: %w", err)
		}
		status.Checked++
		if prevHash != prev {
			status.BrokenID = id
			status.BrokenKind = "prev_hash"
			return status, nil
		}
		if computed != hash {
			status.BrokenID = id
			status.BrokenKind = "hash"
			return status, nil
		}
		prev = hash
	}
	return status, mapError(rows.Err())
}

// EnsureAuditPartitions 预创建前后若干月的审计分区。
func (s *Store) EnsureAuditPartitions(ctx context.Context, aheadMonths int) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT ensure_audit_partitions($1)`, aheadMonths).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: ensure audit partitions: %w", err)
	}
	return n, nil
}

// DropExpiredAuditPartitions 删除超过保留期的整月分区，返回被删除的分区名。
func (s *Store) DropExpiredAuditPartitions(ctx context.Context, retainDays int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT drop_expired_audit_partitions($1)`, retainDays)
	if err != nil {
		return nil, fmt.Errorf("store: drop expired audit partitions: %w", err)
	}
	defer rows.Close()

	var dropped []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: scan dropped partition: %w", err)
		}
		dropped = append(dropped, name)
	}
	return dropped, mapError(rows.Err())
}

// AuditPartitions 返回当前审计分区名，按名称排序（测试与运维巡检使用）。
func (s *Store) AuditPartitions(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_inherits i ON i.inhrelid = c.oid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'audit_logs'
		ORDER BY c.relname`)
	if err != nil {
		return nil, fmt.Errorf("store: list audit partitions: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, mapError(rows.Err())
}

// RetentionDays 读取审计保留天数配置，缺省 180 天。
func (s *Store) RetentionDays(ctx context.Context) (int, error) {
	raw, err := s.GetSetting(ctx, domain.SettingAuditRetentionDays)
	if err != nil {
		return 180, nil
	}
	n, err := strconv.Atoi(strings.Trim(string(raw), `"`))
	if err != nil || n < 1 {
		return 180, nil
	}
	return n, nil
}
