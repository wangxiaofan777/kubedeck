package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

const (
	auditPartitionLock   = "kubedeck_audit_partitions"
	auditPartitionPrefix = "audit_logs_"
	auditPartitionMax    = "audit_logs_max"
)

// EnsureAuditPartitions 预创建当前月前后若干月的审计分区。
// 只拆分比现有最大边界更晚的月份，避免插入早于已有分区的边界。
func (s *Store) EnsureAuditPartitions(ctx context.Context, aheadMonths int) (int, error) {
	if aheadMonths < 0 {
		return 0, fmt.Errorf("store: ahead months must be >= 0, got %d", aheadMonths)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: acquire partition connection: %w", err)
	}
	defer conn.Close()
	if err := acquireLock(ctx, conn, auditPartitionLock, 30); err != nil {
		return 0, err
	}
	defer releaseLock(conn, auditPartitionLock)

	names, err := listAuditPartitions(ctx, conn)
	if err != nil {
		return 0, err
	}
	existing := make(map[string]bool, len(names))
	for _, name := range names {
		existing[name] = true
	}
	if !existing[auditPartitionMax] {
		return 0, fmt.Errorf("store: audit_logs_max partition is missing")
	}
	maxUpper := latestPartitionUpperBound(existing)

	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
	created := 0
	for i := 0; i <= aheadMonths+1; i++ {
		month := start.AddDate(0, i, 0)
		name := auditPartitionName(month)
		upper := month.AddDate(0, 1, 0)
		if existing[name] {
			if upper.After(maxUpper) {
				maxUpper = upper
			}
			continue
		}
		if !upper.After(maxUpper) {
			// 该月份已由更晚的分区覆盖，或早于当前覆盖范围；不重排已有边界。
			continue
		}
		stmt := fmt.Sprintf(
			`ALTER TABLE audit_logs REORGANIZE PARTITION %s INTO (`+
				`PARTITION %s VALUES LESS THAN ('%s'), `+
				`PARTITION %s VALUES LESS THAN (MAXVALUE))`,
			auditPartitionMax, name, upper.Format("2006-01-02 15:04:05.000000"), auditPartitionMax)
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return created, fmt.Errorf("store: create audit partition %s: %w", name, err)
		}
		existing[name] = true
		maxUpper = upper
		created++
	}
	return created, nil
}

// DropExpiredAuditPartitions 删除超过保留期的整月分区，返回被删除的分区名。
func (s *Store) DropExpiredAuditPartitions(ctx context.Context, retainDays int) ([]string, error) {
	if retainDays < 1 {
		return nil, fmt.Errorf("store: retention days must be >= 1, got %d", retainDays)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: acquire partition connection: %w", err)
	}
	defer conn.Close()
	if err := acquireLock(ctx, conn, auditPartitionLock, 30); err != nil {
		return nil, err
	}
	defer releaseLock(conn, auditPartitionLock)

	names, err := listAuditPartitions(ctx, conn)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retainDays)
	var dropped []string
	for _, name := range names {
		if name == auditPartitionMax {
			continue
		}
		month, ok := parseAuditPartitionMonth(name)
		if !ok {
			continue
		}
		if !month.AddDate(0, 1, 0).After(cutoff) {
			stmt := fmt.Sprintf(`ALTER TABLE audit_logs DROP PARTITION %s`, name)
			if _, err := conn.ExecContext(ctx, stmt); err != nil {
				return dropped, fmt.Errorf("store: drop audit partition %s: %w", name, err)
			}
			dropped = append(dropped, name)
		}
	}
	return dropped, nil
}

// AuditPartitions 返回当前审计分区名，按名称排序（测试与运维巡检使用）。
func (s *Store) AuditPartitions(ctx context.Context) ([]string, error) {
	return listAuditPartitions(ctx, s.db)
}

// RetentionDays 读取审计保留天数。
// 「未配置」回退到默认值；读取或解析失败返回错误，调用方必须跳过清理。
func (s *Store) RetentionDays(ctx context.Context) (int, error) {
	raw, err := s.GetSetting(ctx, domain.SettingAuditRetentionDays)
	if errors.Is(err, domain.ErrNotFound) {
		return store.DefaultRetentionDays, nil
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

type queryContext interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func listAuditPartitions(ctx context.Context, q queryContext) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT PARTITION_NAME
		FROM information_schema.PARTITIONS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'audit_logs' AND PARTITION_NAME IS NOT NULL
		ORDER BY PARTITION_NAME`)
	if err != nil {
		return nil, fmt.Errorf("store: list audit partitions: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: scan audit partition: %w", err)
		}
		names = append(names, name)
	}
	return names, mapError(rows.Err())
}

// latestPartitionUpperBound 返回已存在的月份分区中最大的上界（不含兜底分区）。
func latestPartitionUpperBound(names map[string]bool) time.Time {
	var latest time.Time
	for name := range names {
		month, ok := parseAuditPartitionMonth(name)
		if !ok {
			continue
		}
		if upper := month.AddDate(0, 1, 0); upper.After(latest) {
			latest = upper
		}
	}
	return latest
}

func auditPartitionName(ts time.Time) string {
	return auditPartitionPrefix + ts.UTC().Format("200601")
}

func parseAuditPartitionMonth(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, auditPartitionPrefix) {
		return time.Time{}, false
	}
	suffix := strings.TrimPrefix(name, auditPartitionPrefix)
	if len(suffix) != 6 {
		return time.Time{}, false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return time.Time{}, false
		}
	}
	month, err := time.Parse("200601", suffix)
	if err != nil {
		return time.Time{}, false
	}
	return month, true
}
