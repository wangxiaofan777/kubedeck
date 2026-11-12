package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const userColumns = `id, subject, email, display_name, source, is_platform_admin, status, created_at, last_login_at`

func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.Source,
		&u.IsPlatformAdmin, &u.Status, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("store: scan user: %w", err)
	}
	return u, nil
}

// UpsertUser 在首次登录时创建用户，再次登录时刷新邮箱、显示名与登录时间。
func (s *Store) UpsertUser(ctx context.Context, u domain.User) (domain.User, error) {
	if u.Source == "" {
		u.Source = "oidc"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (subject, email, display_name, source, last_login_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (subject) DO UPDATE
		SET email = EXCLUDED.email,
		    display_name = EXCLUDED.display_name,
		    last_login_at = now()
		RETURNING `+userColumns,
		u.Subject, u.Email, u.DisplayName, u.Source)
	user, err := scanUser(row)
	return user, mapError(err)
}

// UserBySubject 按 OIDC subject 查询用户。
func (s *Store) UserBySubject(ctx context.Context, subject string) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE subject = $1`, subject)
	user, err := scanUser(row)
	return user, mapError(err)
}

// UserByID 按主键查询用户。
func (s *Store) UserByID(ctx context.Context, id int64) (domain.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	user, err := scanUser(row)
	return user, mapError(err)
}

// ListUsers 分页返回用户，按 id 排序保证结果稳定。
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]domain.User, error) {
	limit, offset = pageArgs(limit, offset)
	rows, err := s.pool.Query(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0, limit)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, mapError(rows.Err())
}

// CountPlatformAdmins 用于引导流程判断是否已存在管理员（identity-access 初始管理员）。
func (s *Store) CountPlatformAdmins(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE is_platform_admin AND status = 'active'`,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count platform admins: %w", err)
	}
	return n, nil
}

// SetUserStatus 启用/禁用用户（IdP 侧撤销时同步）。
func (s *Store) SetUserStatus(ctx context.Context, id int64, status string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// SetPlatformAdmin 调整平台管理员标记。
func (s *Store) SetPlatformAdmin(ctx context.Context, id int64, admin bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET is_platform_admin = $2 WHERE id = $1`, id, admin)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func pageArgs(limit, offset int) (int, int) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
