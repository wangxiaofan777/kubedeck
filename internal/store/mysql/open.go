// Package mysql 是 MySQL 8 的元数据仓储实现。
//
// 只存平台元数据（用户、集群、凭据密文、角色绑定、审计），
// 集群资源状态一律不入库。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const (
	poolMaxOpen        = 10
	poolMaxIdle        = 5
	poolConnLifetime   = 30 * time.Minute
	poolConnIdleTime   = 5 * time.Minute
	connectPingTimeout = 5 * time.Second
)

// Store 是 MySQL 仓储入口。
type Store struct {
	db *sql.DB
}

// Driver 返回引擎标识。
func (s *Store) Driver() string { return "mysql" }

// Open 建立连接池并做一次连通性检查。
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("store: database URL is empty")
	}
	cfg, err := parseDSN(databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("store: open mysql: %w", err)
	}
	db.SetMaxOpenConns(poolMaxOpen)
	db.SetMaxIdleConns(poolMaxIdle)
	db.SetConnMaxLifetime(poolConnLifetime)
	db.SetConnMaxIdleTime(poolConnIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, connectPingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 释放连接池。
func (s *Store) Close() { _ = s.db.Close() }

// DB 暴露连接池给迁移与集成测试使用。
func (s *Store) DB() *sql.DB { return s.db }

// Ping 检查数据库可用性。
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

// parseDSN 接受 mysql:// URL 或 go-sql-driver 原生 DSN，并强制时区/时间解析设置。
func parseDSN(raw string) (*mysqldriver.Config, error) {
	raw = strings.TrimSpace(raw)
	var (
		cfg *mysqldriver.Config
		err error
	)
	if strings.HasPrefix(strings.ToLower(raw), "mysql://") {
		cfg, err = parseURL(raw)
	} else {
		cfg, err = mysqldriver.ParseDSN(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("store: parse mysql DSN: %w", err)
	}
	if cfg.DBName == "" {
		return nil, errors.New("store: mysql DSN must include a database name")
	}
	// 平台所有时间以 UTC 读写；JSON/分页/分区都依赖这一约定。
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = true
	// 与 PostgreSQL 对齐：UPDATE 的 RowsAffected 表示匹配行数，而不是实际变更行数。
	cfg.ClientFoundRows = true
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	return cfg, nil
}

func parseURL(raw string) (*mysqldriver.Config, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("store: parse mysql URL: %w", err)
	}
	if strings.ToLower(u.Scheme) != "mysql" {
		return nil, fmt.Errorf("store: unsupported mysql URL scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("store: mysql URL must include a host")
	}
	port := u.Port()
	if port == "" {
		port = "3306"
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, errors.New("store: mysql URL must include a database name")
	}

	// 借用驱动的 DSN 解析器处理 query 参数（charset、tls 等），再覆盖用户信息。
	synthetic := fmt.Sprintf("u:p@tcp(%s)/%s", net.JoinHostPort(host, port), dbName)
	if u.RawQuery != "" {
		synthetic += "?" + u.RawQuery
	}
	cfg, err := mysqldriver.ParseDSN(synthetic)
	if err != nil {
		return nil, fmt.Errorf("store: parse mysql URL options: %w", err)
	}
	cfg.User = u.User.Username()
	if pass, ok := u.User.Password(); ok {
		cfg.Passwd = pass
	}
	return cfg, nil
}

// mapError 把 MySQL 错误映射为领域错误。
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrConflict) {
		return err
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1062: // ER_DUP_ENTRY
			return fmt.Errorf("%w: %s", domain.ErrConflict, mysqlErr.Message)
		case 1451, 1452: // ER_ROW_IS_REFERENCED / ER_NO_REFERENCED_ROW
			return fmt.Errorf("%w: %s", domain.ErrConflict, mysqlErr.Message)
		}
	}
	return err
}

// rowScanner 是 *sql.Row 与 *sql.Rows 的公共扫描接口。
type rowScanner interface {
	Scan(dest ...any) error
}
