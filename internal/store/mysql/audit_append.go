package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

// appendAuditSQL 写入一条审计记录；client_ip 为空串时写 NULL，与 PostgreSQL 语义一致。
const appendAuditSQL = `
	INSERT INTO audit_logs (
		occurred_at, request_id, actor_id, actor_subject, actor_email,
		cluster_id, cluster_name, verb, api_group, resource, namespace, name,
		result, result_code, latency_ms, client_ip, user_agent, body_hash, detail, prev_hash, hash
	) VALUES (
		?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?
	)`

// AppendAudit 追加一条审计记录，并在同一事务内把它挂到哈希链尾部。
func (s *Store) AppendAudit(ctx context.Context, e domain.AuditEntry) (domain.AuditRecord, error) {
	records, err := s.appendAudit(ctx, []domain.AuditEntry{e})
	if err != nil {
		return domain.AuditRecord{}, err
	}
	return records[0], nil
}

// AppendAuditBatch 在一个事务内追加多条记录，哈希链按顺序衔接。
// 任一条失败则整批回滚，由调用方进入补偿重试。
func (s *Store) AppendAuditBatch(ctx context.Context, entries []domain.AuditEntry) ([]domain.AuditRecord, error) {
	return s.appendAudit(ctx, entries)
}

func (s *Store) appendAudit(ctx context.Context, entries []domain.AuditEntry) ([]domain.AuditRecord, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin audit tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 行锁串行化哈希链追加，避免并发写入产生分叉链。
	var lockName string
	if err := tx.QueryRowContext(ctx,
		`SELECT name FROM store_locks WHERE name = 'audit_chain' FOR UPDATE`).Scan(&lockName); err != nil {
		return nil, fmt.Errorf("store: lock audit chain: %w", err)
	}
	prevHash, err := auditTailHash(ctx, tx)
	if err != nil {
		return nil, err
	}

	records := make([]domain.AuditRecord, 0, len(entries))
	for _, entry := range entries {
		e, detail, err := store.PrepareAuditEntry(entry)
		if err != nil {
			return nil, err
		}
		canonical, err := store.CanonicalAuditDetail(detail)
		if err != nil {
			return nil, err
		}
		e.PrevHash = prevHash
		hash := store.ComputeAuditHash(e, canonical)

		res, err := tx.ExecContext(ctx, appendAuditSQL,
			e.OccurredAt, e.RequestID, e.ActorID, e.ActorSubject, e.ActorEmail,
			e.ClusterID, e.ClusterName, e.Verb, e.APIGroup, e.Resource, e.Namespace, e.ObjectName,
			e.Result, e.ResultCode, e.LatencyMS, e.ClientIP, e.UserAgent, e.BodyHash,
			string(detail), e.PrevHash, hash)
		if err != nil {
			return nil, fmt.Errorf("store: insert audit log: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("store: audit log id: %w", err)
		}
		prevHash = hash
		records = append(records, domain.AuditRecord{
			ID:         id,
			OccurredAt: e.OccurredAt,
			PrevHash:   e.PrevHash,
			Hash:       hash,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit audit batch: %w", err)
	}
	return records, nil
}

// auditTailHash 读取当前链尾哈希；空表返回空串。
func auditTailHash(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (string, error) {
	var hash string
	err := q.QueryRowContext(ctx, `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: read audit chain tail: %w", err)
	}
	return hash, nil
}
