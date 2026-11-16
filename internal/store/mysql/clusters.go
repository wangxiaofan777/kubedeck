package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const clusterColumns = `id, name, display_name, server, transport, auth_mode, server_version,
	status, message, labels, last_seen_at, created_by, created_at, updated_at`

func scanCluster(row rowScanner) (domain.Cluster, error) {
	var (
		c      domain.Cluster
		status string
		labels []byte
	)
	err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.Server, &c.Transport, &c.AuthMode,
		&c.ServerVersion, &status, &c.Message, &labels, &c.LastSeenAt, &c.CreatedBy,
		&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Cluster{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: scan cluster: %w", err)
	}
	c.Status = domain.ClusterStatus(status)
	if len(labels) > 0 {
		if err := json.Unmarshal(labels, &c.Labels); err != nil {
			return domain.Cluster{}, fmt.Errorf("store: decode cluster labels: %w", err)
		}
	}
	return c, nil
}

// CreateCluster 登记集群。name 唯一冲突返回 domain.ErrConflict。
func (s *Store) CreateCluster(ctx context.Context, c domain.Cluster) (domain.Cluster, error) {
	labels, err := json.Marshal(orEmptyLabels(c.Labels))
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: encode cluster labels: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO clusters (name, display_name, server, transport, auth_mode,
		                      server_version, status, message, labels, created_by)
		VALUES (?, COALESCE(NULLIF(?, ''), ?), ?,
		        COALESCE(NULLIF(?, ''), 'direct'),
		        COALESCE(NULLIF(?, ''), 'impersonation'),
		        ?, COALESCE(NULLIF(?, ''), 'unknown'), ?, ?, ?)`,
		c.Name, c.DisplayName, c.Name, c.Server, c.Transport, c.AuthMode,
		c.ServerVersion, string(c.Status), c.Message, string(labels), c.CreatedBy)
	if err != nil {
		return domain.Cluster{}, mapError(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: create cluster id: %w", err)
	}
	return s.ClusterByID(ctx, id)
}

// ClusterByName 按名称查询集群。
func (s *Store) ClusterByName(ctx context.Context, name string) (domain.Cluster, error) {
	c, err := scanCluster(s.db.QueryRowContext(ctx,
		`SELECT `+clusterColumns+` FROM clusters WHERE name = ?`, name))
	return c, mapError(err)
}

// ClusterByID 按主键查询集群。
func (s *Store) ClusterByID(ctx context.Context, id int64) (domain.Cluster, error) {
	c, err := scanCluster(s.db.QueryRowContext(ctx,
		`SELECT `+clusterColumns+` FROM clusters WHERE id = ?`, id))
	return c, mapError(err)
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
		WHERE rb.user_id = ?
		ORDER BY c.name`, userID)
}

func (s *Store) queryClusters(ctx context.Context, query string, args ...any) ([]domain.Cluster, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	res, err := s.db.ExecContext(ctx, `
		UPDATE clusters
		SET status = ?,
		    message = ?,
		    server_version = COALESCE(NULLIF(?, ''), server_version),
		    last_seen_at = ?,
		    updated_at = UTC_TIMESTAMP(6)
		WHERE id = ?`, string(status), message, version, seenAt.UTC(), id)
	if err != nil {
		return mapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
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
	res, err := s.db.ExecContext(ctx, `
		UPDATE clusters
		SET display_name = COALESCE(NULLIF(?, ''), display_name),
		    labels = ?,
		    updated_at = UTC_TIMESTAMP(6)
		WHERE id = ?`, displayName, string(b), id)
	if err != nil {
		return mapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteCluster 移除集群登记；凭据与绑定随外键级联删除，审计记录保留。
func (s *Store) DeleteCluster(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM clusters WHERE id = ?`, id)
	if err != nil {
		return mapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
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

// CreateClusterWithCredential 在同一事务内创建集群与凭据，避免出现无凭据的登记。
func (s *Store) CreateClusterWithCredential(ctx context.Context, c domain.Cluster, cred domain.Credential) (domain.Cluster, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: begin create cluster: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	labels, err := json.Marshal(orEmptyLabels(c.Labels))
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: encode cluster labels: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO clusters (name, display_name, server, transport, auth_mode,
		                      server_version, status, message, labels, created_by)
		VALUES (?, COALESCE(NULLIF(?, ''), ?), ?,
		        COALESCE(NULLIF(?, ''), 'direct'),
		        COALESCE(NULLIF(?, ''), 'impersonation'),
		        ?, COALESCE(NULLIF(?, ''), 'unknown'), ?, ?, ?)`,
		c.Name, c.DisplayName, c.Name, c.Server, c.Transport, c.AuthMode,
		c.ServerVersion, string(c.Status), c.Message, string(labels), c.CreatedBy)
	if err != nil {
		return domain.Cluster{}, mapError(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Cluster{}, fmt.Errorf("store: create cluster id: %w", err)
	}
	cluster, err := scanCluster(tx.QueryRowContext(ctx,
		`SELECT `+clusterColumns+` FROM clusters WHERE id = ?`, id))
	if err != nil {
		return domain.Cluster{}, mapError(err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cluster_credentials
		    (cluster_id, ciphertext, nonce, key_version, auth_type, fingerprint, rotated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, UTC_TIMESTAMP(6), ?)`,
		cluster.ID, cred.Ciphertext, cred.Nonce, cred.KeyVersion, cred.AuthType,
		cred.Fingerprint, cred.ExpiresAt); err != nil {
		return domain.Cluster{}, mapError(err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Cluster{}, fmt.Errorf("store: commit create cluster: %w", err)
	}
	return cluster, nil
}

// ClusterCredentialMeta 返回凭据元数据（含密文），仅供服务端解密与展示指纹。
func (s *Store) ClusterCredentialMeta(ctx context.Context, clusterID int64) (domain.Credential, error) {
	return s.CredentialByClusterID(ctx, clusterID)
}

// UpdateClusterConnection 在凭据轮换后同步 server、版本与状态。
func (s *Store) UpdateClusterConnection(ctx context.Context, id int64, server, version, status, message string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE clusters
		SET server = ?,
		    server_version = COALESCE(NULLIF(?, ''), server_version),
		    status = COALESCE(NULLIF(?, ''), status),
		    message = ?,
		    updated_at = UTC_TIMESTAMP(6)
		WHERE id = ?`, server, version, status, message, id)
	if err != nil {
		return mapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
