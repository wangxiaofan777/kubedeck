package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const defaultRetentionDays = 180

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

// RetentionDays 读取审计保留天数。
// 「未配置」回退到默认值；读取或解析失败返回错误，调用方必须跳过清理，
// 不能用默认值替代已配置策略，否则可能误删仍在保留期内的审计数据。
func (s *Store) RetentionDays(ctx context.Context) (int, error) {
	raw, err := s.GetSetting(ctx, domain.SettingAuditRetentionDays)
	if errors.Is(err, domain.ErrNotFound) {
		return defaultRetentionDays, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.Trim(string(raw), `"`))
	if err != nil {
		return 0, fmt.Errorf("store: invalid retention days %q: %w", raw, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("store: retention days must be >= 1, got %d", n)
	}
	return n, nil
}
