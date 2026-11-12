package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// auditChainLockKey 串行化哈希链追加，避免并发写入产生分叉链。
const auditChainLockKey int64 = 0x6175646974636861 // "auditcha"

const auditHashArgs = `$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
	$14, $15, NULLIF($16, '')::inet, $17, $18, $19, $20`

// AppendAudit 追加一条审计记录，并在同一事务内把它挂到哈希链尾部。
// 审计记录的结果不改变业务返回值；调用方负责失败时的补偿队列。
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

// AppendAuditBatch 在一个事务内追加多条记录，哈希链按顺序衔接。
// 任一条失败则整批回滚，由调用方进入补偿重试。
func (s *Store) AppendAuditBatch(ctx context.Context, entries []domain.AuditEntry) ([]domain.AuditRecord, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin audit batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditChainLockKey); err != nil {
		return nil, fmt.Errorf("store: lock audit chain: %w", err)
	}
	var prevHash string
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&prevHash)
	if errors.Is(err, pgx.ErrNoRows) {
		prevHash = ""
	} else if err != nil {
		return nil, fmt.Errorf("store: read audit chain tail: %w", err)
	}

	records := make([]domain.AuditRecord, 0, len(entries))
	for _, e := range entries {
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
			return nil, fmt.Errorf("store: encode audit detail: %w", err)
		}
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
			detail, prevHash,
		).Scan(&rec.ID, &rec.OccurredAt, &rec.PrevHash, &rec.Hash)
		if err != nil {
			return nil, fmt.Errorf("store: insert audit batch row: %w", err)
		}
		prevHash = rec.Hash
		records = append(records, rec)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit audit batch: %w", err)
	}
	return records, nil
}
