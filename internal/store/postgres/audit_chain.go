package postgres

import (
	"context"
	"fmt"

	"github.com/kubedeck/kubedeck/internal/store"
)

// VerifyAuditChain 逐条重算哈希并检查前后衔接，返回第一个断裂点（如有）。
// 保留期删除最老分区后，第一条存活记录的 prev_hash 指向已删除的记录；
// 这种情况标记 Truncated 并继续校验存活区间，而不是误报为篡改。
func (s *Store) VerifyAuditChain(ctx context.Context) (store.ChainStatus, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, hash, prev_hash,
		       audit_log_hash(occurred_at, request_id, actor_id, actor_subject, actor_email,
		                      cluster_id, cluster_name, verb, api_group, resource, namespace, name,
		                      result, result_code, latency_ms, client_ip, user_agent,
		                      body_hash, detail, prev_hash) AS computed
		FROM audit_logs ORDER BY id`)
	if err != nil {
		return store.ChainStatus{}, fmt.Errorf("store: verify audit chain: %w", err)
	}
	defer rows.Close()

	var (
		status store.ChainStatus
		prev   string
		first  = true
	)
	for rows.Next() {
		var (
			id       int64
			hash     string
			prevHash string
			computed string
		)
		if err := rows.Scan(&id, &hash, &prevHash, &computed); err != nil {
			return store.ChainStatus{}, fmt.Errorf("store: scan audit chain row: %w", err)
		}
		status.Checked++
		if first {
			// 链的起点可能因保留期删除而不可见；只要当前记录自身哈希正确即可继续。
			first = false
			if prevHash != prev {
				status.Truncated = true
			}
		} else if prevHash != prev {
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
