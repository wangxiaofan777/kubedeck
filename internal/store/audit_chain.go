package store

import (
	"context"
	"fmt"
)

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
