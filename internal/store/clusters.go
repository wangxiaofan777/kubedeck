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

const clusterColumns = `id, name, display_name, server, transport, auth_mode, server_version,
	status, message, labels, last_seen_at, created_by, created_at, updated_at`

func scanCluster(row pgx.Row) (domain.Cluster, error) {
	var (
		c      domain.Cluster
		labels []byte
	)
	err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.Server, &c.Transport, &c.AuthMode,
		&c.ServerVersion, &c.Status, &c.Message, &labels, &c.LastSeenAt, &c.CreatedBy,
		&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Cluster{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: scan cluster: %w", err)
	}
	if len(labels) > 0 {
		if err := json.Unmarshal(labels, &c.Labels); err != nil {
			return domain.Cluster{}, fmt.Errorf("store: decode cluster labels: %w", err)
		}
	}
	return c, nil
}

// CreateCluster 登记集群。name 唯一冲突返回 domain.ErrConflict（接入边界与冲突处理）。
func (s *Store) CreateCluster(ctx context.Context, c domain.Cluster) (domain.Cluster, error) {
	labels, err := json.Marshal(orEmptyLabels(c.Labels))
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: encode cluster labels: %w", err)
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO clusters (name, display_name, server, transport, auth_mode,
		                      server_version, status, message, labels, created_by)
		VALUES ($1, COALESCE(NULLIF($2, ''), $1), $3,
		        COALESCE(NULLIF($4, ''), 'direct'),
		        COALESCE(NULLIF($5, ''), 'impersonation'),
		        $6, COALESCE(NULLIF($7, ''), 'unknown'), $8, $9, $10)
		RETURNING `+clusterColumns,
		c.Name, c.DisplayName, c.Server, c.Transport, c.AuthMode,
		c.ServerVersion, string(c.Status), c.Message, labels, c.CreatedBy)
	cluster, err := scanCluster(row)
	return cluster, mapError(err)
}

// ClusterByName 按名称查询集群。
func (s *Store) ClusterByName(ctx context.Context, name string) (domain.Cluster, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+clusterColumns+` FROM clusters WHERE name = $1`, name)
	cluster, err := scanCluster(row)
	return cluster, mapError(err)
}

// ClusterByID 按主键查询集群。
func (s *Store) ClusterByID(ctx context.Context, id int64) (domain.Cluster, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+clusterColumns+` FROM clusters WHERE id = $1`, id)
	cluster, err := scanCluster(row)
	return cluster, mapError(err)
}

// Clusters 返回全部集群（平台管理员视角）。
func (s *Store) Clusters(ctx context.Context) ([]domain.Cluster, error) {
	return s.queryClusters(ctx,
		`SELECT `+clusterColumns+` FROM clusters ORDER BY name`)
}

// ClustersForUser 只返回用户有角色绑定的集群（集群可见范围）。
func (s *Store) ClustersForUser(ctx context.Context, userID int64) ([]domain.Cluster, error) {
	return s.queryClusters(ctx, `
		SELECT DISTINCT c.id, c.name, c.display_name, c.server, c.transport, c.auth_mode,
		       c.server_version, c.status, c.message, c.labels, c.last_seen_at,
		       c.created_by, c.created_at, c.updated_at
		FROM clusters c
		JOIN role_bindings rb ON rb.cluster_id = c.id
		WHERE rb.user_id = $1
		ORDER BY c.name`, userID)
}

func (s *Store) queryClusters(ctx context.Context, sql string, args ...any) ([]domain.Cluster, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list clusters: %w", err)
	}
	defer rows.Close()

	clusters := make([]domain.Cluster, 0)
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, c)
	}
	return clusters, mapError(rows.Err())
}

// UpdateClusterStatus 写入健康探测结果；version 为空时保留已有版本。
func (s *Store) UpdateClusterStatus(ctx context.Context, id int64, status domain.ClusterStatus, message, version string, seenAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE clusters
		SET status = $2,
		    message = $3,
		    server_version = COALESCE(NULLIF($4, ''), server_version),
		    last_seen_at = $5,
		    updated_at = now()
		WHERE id = $1`, id, string(status), message, version, seenAt)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RenameCluster 修改展示名与标签。
func (s *Store) RenameCluster(ctx context.Context, id int64, displayName string, labels map[string]string) error {
	b, err := json.Marshal(orEmptyLabels(labels))
	if err != nil {
		return fmt.Errorf("store: encode cluster labels: %w", err)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE clusters
		SET display_name = COALESCE(NULLIF($2, ''), display_name),
		    labels = $3,
		    updated_at = now()
		WHERE id = $1`, id, displayName, b)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteCluster 移除集群登记；凭据与绑定随外键级联删除，审计记录保留（无外键）。
func (s *Store) DeleteCluster(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM clusters WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func orEmptyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	return labels
}
