// Package store 负责 PostgreSQL 连接、迁移与平台元数据的持久化。
//
// 只存平台元数据（用户、集群、凭据密文、角色绑定、审计），
// 集群资源状态一律不入库（design D6）。
package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockKey 是迁移的 advisory lock 键，防止多副本同时启动时并发建表。
const migrationLockKey int64 = 0x6b6465636b // "kdeck"

// Migrate 按文件名顺序应用尚未执行的迁移，每个文件在同一个事务内完成。
// 已应用的迁移会校验文件校验和：内容被改动时停止启动，避免环境间 schema 漂移。
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			checksum   TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// 兼容引入校验和之前的数据库：启动时补列，存量记录在下方回填。
	if _, err := tx.Exec(ctx, `
		ALTER TABLE schema_migrations
		ADD COLUMN IF NOT EXISTS checksum TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add schema_migrations.checksum: %w", err)
	}

	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(path.Base(file), ".sql")
		body, err := migrationsFS.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}
		sum := fileChecksum(body)

		var applied string
		err = tx.QueryRow(ctx,
			"SELECT checksum FROM schema_migrations WHERE version = $1", version).Scan(&applied)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// 不带参数的 Exec 走简单查询协议，允许一个文件包含多条语句。
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("apply migration %s: %w", version, err)
			}
			if _, err := tx.Exec(ctx,
				"INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", version, sum,
			); err != nil {
				return fmt.Errorf("record migration %s: %w", version, err)
			}
		case err != nil:
			return fmt.Errorf("check migration %s: %w", version, err)
		case applied == "":
			// 引入校验和前已应用的记录：用当前文件内容回填。
			if _, err := tx.Exec(ctx,
				"UPDATE schema_migrations SET checksum = $2 WHERE version = $1", version, sum,
			); err != nil {
				return fmt.Errorf("backfill checksum for %s: %w", version, err)
			}
		case applied != sum:
			return fmt.Errorf(
				"migration %s changed after it was applied (checksum %s, expected %s); add a new migration instead",
				version, sum, applied)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

// AppliedMigrations 返回已应用的迁移版本，供启动日志与测试断言使用。
func AppliedMigrations(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func fileChecksum(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
