package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const credentialColumns = `cluster_id, ciphertext, nonce, key_version, auth_type, fingerprint, rotated_at, expires_at`

func scanCredential(row pgx.Row) (domain.Credential, error) {
	var c domain.Credential
	err := row.Scan(&c.ClusterID, &c.Ciphertext, &c.Nonce, &c.KeyVersion,
		&c.AuthType, &c.Fingerprint, &c.RotatedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credential{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Credential{}, fmt.Errorf("store: scan credential: %w", err)
	}
	return c, nil
}

// ReplaceCredential 写入或轮换集群凭据。原地覆盖，旧凭据立即失效。
func (s *Store) ReplaceCredential(ctx context.Context, c domain.Credential) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cluster_credentials
		    (cluster_id, ciphertext, nonce, key_version, auth_type, fingerprint, rotated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), $7)
		ON CONFLICT (cluster_id) DO UPDATE
		SET ciphertext = EXCLUDED.ciphertext,
		    nonce = EXCLUDED.nonce,
		    key_version = EXCLUDED.key_version,
		    auth_type = EXCLUDED.auth_type,
		    fingerprint = EXCLUDED.fingerprint,
		    rotated_at = now(),
		    expires_at = EXCLUDED.expires_at`,
		c.ClusterID, c.Ciphertext, c.Nonce, c.KeyVersion, c.AuthType, c.Fingerprint, c.ExpiresAt)
	return mapError(err)
}

// CredentialByClusterID 读取集群凭据密文（仅服务端内部使用，绝不出现在 API 响应）。
func (s *Store) CredentialByClusterID(ctx context.Context, clusterID int64) (domain.Credential, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+credentialColumns+` FROM cluster_credentials WHERE cluster_id = $1`, clusterID)
	c, err := scanCredential(row)
	return c, mapError(err)
}

// CredentialsByKeyVersion 返回仍使用旧主密钥的凭据，供轮换任务逐条推进（D7）。
func (s *Store) CredentialsByKeyVersion(ctx context.Context, keyVersion int) ([]domain.Credential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+credentialColumns+` FROM cluster_credentials WHERE key_version = $1 ORDER BY cluster_id`,
		keyVersion)
	if err != nil {
		return nil, fmt.Errorf("store: list credentials by key version: %w", err)
	}
	defer rows.Close()

	credentials := make([]domain.Credential, 0)
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, c)
	}
	return credentials, mapError(rows.Err())
}

// CountCredentials 统计凭据数量，用于健康检查与测试。
func (s *Store) CountCredentials(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM cluster_credentials`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count credentials: %w", err)
	}
	return n, nil
}
