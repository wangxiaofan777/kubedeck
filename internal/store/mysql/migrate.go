package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const (
	migrationLockName    = "kubedeck_migrate"
	migrationLockTimeout = 30
)

// Migrate 按文件名顺序应用尚未执行的迁移。
//
// MySQL 的 DDL 会隐式提交，无法把全部迁移包在单个事务里；因此迁移文件内的语句
// 必须写成幂等的，失败后重跑即可从已完成部分继续。并发启动由 GET_LOCK 串行化。
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: acquire migration connection: %w", err)
	}
	defer conn.Close()

	if err := acquireLock(ctx, conn, migrationLockName, migrationLockTimeout); err != nil {
		return err
	}
	defer releaseLock(conn, migrationLockName)

	if err := ensureMigrationTable(ctx, conn); err != nil {
		return err
	}

	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("store: list migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(path.Base(file), ".sql")
		body, err := migrationsFS.ReadFile(file)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", version, err)
		}
		sum := fileChecksum(body)

		var applied string
		err = conn.QueryRowContext(ctx,
			"SELECT checksum FROM schema_migrations WHERE version = ?", version).Scan(&applied)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := conn.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("store: apply migration %s: %w", version, migrationErrorHint(err))
			}
			if _, err := conn.ExecContext(ctx,
				"INSERT INTO schema_migrations (version, checksum) VALUES (?, ?)", version, sum,
			); err != nil {
				return fmt.Errorf("store: record migration %s: %w", version, err)
			}
		case err != nil:
			return fmt.Errorf("store: check migration %s: %w", version, err)
		case applied == "":
			if _, err := conn.ExecContext(ctx,
				"UPDATE schema_migrations SET checksum = ? WHERE version = ?", sum, version,
			); err != nil {
				return fmt.Errorf("store: backfill checksum for %s: %w", version, err)
			}
		case applied != sum:
			return fmt.Errorf(
				"store: migration %s changed after it was applied (checksum %s, expected %s); add a new migration instead",
				version, sum, applied)
		}
	}
	return nil
}

func ensureMigrationTable(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(191) NOT NULL,
			applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			checksum   VARCHAR(64) NOT NULL DEFAULT '',
			PRIMARY KEY (version)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}
	return nil
}

// AppliedMigrations 返回已应用的迁移版本，供启动日志与测试断言使用。
func (s *Store) AppliedMigrations(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("store: query schema_migrations: %w", err)
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

// migrationErrorHint 为常见部署缺失给出可操作的提示。
// MySQL 开启 binlog 时创建触发器需要 log_bin_trust_function_creators=1 或 SUPER。
func migrationErrorHint(err error) error {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1419 {
		return fmt.Errorf(
			"trigger creation requires log_bin_trust_function_creators=1 or SUPER privilege: %w", err)
	}
	return err
}

// acquireLock 在固定连接上获取 MySQL 会话级命名锁。
func acquireLock(ctx context.Context, conn *sql.Conn, name string, timeoutSeconds int) error {
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, timeoutSeconds).Scan(&got); err != nil {
		return fmt.Errorf("store: acquire lock %s: %w", name, err)
	}
	if !got.Valid || got.Int64 != 1 {
		return fmt.Errorf("store: acquire lock %s: timed out after %ds", name, timeoutSeconds)
	}
	return nil
}

// releaseLock 释放会话级锁；使用独立超时，避免调用方 ctx 已取消时泄漏。
func releaseLock(conn *sql.Conn, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var released sql.NullInt64
	_ = conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", name).Scan(&released)
}
