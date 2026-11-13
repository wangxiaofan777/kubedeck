package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AuditStatsFilter 是审计统计范围；From/To 必填，ClusterName 为空表示全部集群。
type AuditStatsFilter struct {
	From        time.Time
	To          time.Time
	ClusterName string
}

// AuditCount 是单个分组键的计数。
type AuditCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// AuditDayCount 是按天汇总的成功/失败计数。
type AuditDayCount struct {
	Day    string `json:"day"`
	Total  int    `json:"total"`
	Failed int    `json:"failed"`
}

// AuditStats 是审计聚合结果，只包含计数与分组键。
type AuditStats struct {
	From       time.Time       `json:"from"`
	To         time.Time       `json:"to"`
	Total      int             `json:"total"`
	Success    int             `json:"success"`
	Failed     int             `json:"failed"`
	ByDay      []AuditDayCount `json:"byDay"`
	ByVerb     []AuditCount    `json:"byVerb"`
	ByResource []AuditCount    `json:"byResource"`
	ByActor    []AuditCount    `json:"byActor"`
}

// auditStatsWhere 构造统计过滤条件；时间范围是必需条件，可触发分区裁剪。
func auditStatsWhere(f AuditStatsFilter) (string, []any) {
	where := []string{"occurred_at >= $1", "occurred_at < $2"}
	args := []any{f.From.UTC(), f.To.UTC()}
	if f.ClusterName != "" {
		args = append(args, f.ClusterName)
		where = append(where, fmt.Sprintf("cluster_name = $%d", len(args)))
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// AuditStats 汇总指定时间范围内的审计记录。
func (s *Store) AuditStats(ctx context.Context, f AuditStatsFilter) (AuditStats, error) {
	clause, args := auditStatsWhere(f)
	stats := AuditStats{
		From:       f.From.UTC(),
		To:         f.To.UTC(),
		ByDay:      []AuditDayCount{},
		ByVerb:     []AuditCount{},
		ByResource: []AuditCount{},
		ByActor:    []AuditCount{},
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT count(*),
		        count(*) FILTER (WHERE result = 'success'),
		        count(*) FILTER (WHERE result <> 'success')
		 FROM audit_logs`+clause, args...).
		Scan(&stats.Total, &stats.Success, &stats.Failed); err != nil {
		return AuditStats{}, fmt.Errorf("store: audit stats total: %w", err)
	}

	dayRows, err := s.pool.Query(ctx,
		`SELECT to_char(date_trunc('day', occurred_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'),
		        count(*),
		        count(*) FILTER (WHERE result <> 'success')
		 FROM audit_logs`+clause+` GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return AuditStats{}, fmt.Errorf("store: audit stats by day: %w", err)
	}
	for dayRows.Next() {
		var item AuditDayCount
		if err := dayRows.Scan(&item.Day, &item.Total, &item.Failed); err != nil {
			dayRows.Close()
			return AuditStats{}, fmt.Errorf("store: scan audit stats by day: %w", err)
		}
		stats.ByDay = append(stats.ByDay, item)
	}
	dayRows.Close()
	if err := dayRows.Err(); err != nil {
		return AuditStats{}, fmt.Errorf("store: audit stats by day rows: %w", err)
	}

	if stats.ByVerb, err = s.auditCountBy(ctx, "verb", clause, args, 20); err != nil {
		return AuditStats{}, err
	}
	if stats.ByResource, err = s.auditCountBy(ctx, "resource", clause, args, 10); err != nil {
		return AuditStats{}, err
	}
	if stats.ByActor, err = s.auditCountBy(ctx, "actor_subject", clause, args, 10); err != nil {
		return AuditStats{}, err
	}
	return stats, nil
}

// auditCountBy 按单个列分组取 Top N；列名来自内部常量，不接受外部输入。
func (s *Store) auditCountBy(
	ctx context.Context,
	column, clause string,
	args []any,
	limit int,
) ([]AuditCount, error) {
	queryArgs := append(append([]any{}, args...), limit)
	sql := fmt.Sprintf(
		`SELECT %s, count(*) FROM audit_logs%s GROUP BY 1 ORDER BY 2 DESC, 1 ASC LIMIT $%d`,
		column, clause, len(queryArgs),
	)
	rows, err := s.pool.Query(ctx, sql, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("store: audit stats by %s: %w", column, err)
	}
	defer rows.Close()

	out := make([]AuditCount, 0, limit)
	for rows.Next() {
		var item AuditCount
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, fmt.Errorf("store: scan audit stats by %s: %w", column, err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: audit stats by %s rows: %w", column, err)
	}
	return out, nil
}
