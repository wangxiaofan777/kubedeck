package mysql

import (
	"context"
	"fmt"

	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

// VerifyAuditChain 逐条重算哈希并检查前后衔接，返回第一个断裂点（如有）。
// MySQL 的哈希在 Go 侧计算，因此这里重新读取原始字段后按同一算法复算。
func (s *Store) VerifyAuditChain(ctx context.Context) (store.ChainStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, occurred_at, request_id, actor_id, actor_subject, actor_email,
		       cluster_id, cluster_name, verb, api_group, resource, namespace, name,
		       result, result_code, latency_ms, COALESCE(client_ip, ''), user_agent,
		       body_hash, detail, prev_hash, hash
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
			e      domain.AuditEntry
			detail []byte
		)
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.RequestID, &e.ActorID, &e.ActorSubject, &e.ActorEmail,
			&e.ClusterID, &e.ClusterName, &e.Verb, &e.APIGroup, &e.Resource, &e.Namespace, &e.ObjectName,
			&e.Result, &e.ResultCode, &e.LatencyMS, &e.ClientIP, &e.UserAgent,
			&e.BodyHash, &detail, &e.PrevHash, &e.Hash); err != nil {
			return store.ChainStatus{}, fmt.Errorf("store: scan audit chain row: %w", err)
		}
		canonical, err := store.CanonicalAuditDetail(detail)
		if err != nil {
			return store.ChainStatus{}, err
		}
		computed := store.ComputeAuditHash(e, canonical)

		status.Checked++
		if first {
			// 链的起点可能因保留期删除而不可见；只要当前记录自身哈希正确即可继续。
			first = false
			if e.PrevHash != prev {
				status.Truncated = true
			}
		} else if e.PrevHash != prev {
			status.BrokenID = e.ID
			status.BrokenKind = "prev_hash"
			return status, nil
		}
		if computed != e.Hash {
			status.BrokenID = e.ID
			status.BrokenKind = "hash"
			return status, nil
		}
		prev = e.Hash
	}
	return status, mapError(rows.Err())
}
