package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/store"
)

const userColumns = `id, subject, email, display_name, source, is_platform_admin, status, created_at, last_login_at`

func scanUser(row rowScanner) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Subject, &u.Email, &u.DisplayName, &u.Source,
		&u.IsPlatformAdmin, &u.Status, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
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
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (subject, email, display_name, source, last_login_at)
		VALUES (?, ?, ?, ?, UTC_TIMESTAMP(6)) AS new
		ON DUPLICATE KEY UPDATE
			id = LAST_INSERT_ID(id),
			email = new.email,
			display_name = new.display_name,
			last_login_at = new.last_login_at`,
		u.Subject, u.Email, u.DisplayName, u.Source)
	if err != nil {
		return domain.User{}, mapError(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.User{}, fmt.Errorf("store: upsert user id: %w", err)
	}
	return s.UserByID(ctx, id)
}

// UserBySubject 按 OIDC subject 查询用户。
func (s *Store) UserBySubject(ctx context.Context, subject string) (domain.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE subject = ?`, subject))
	return u, mapError(err)
}

// UserByID 按主键查询用户。
func (s *Store) UserByID(ctx context.Context, id int64) (domain.User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	return u, mapError(err)
}

// ListUsers 分页返回用户，按 id 排序保证结果稳定。
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]domain.User, error) {
	limit, offset = store.PageArgs(limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
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

// CountPlatformAdmins 用于引导流程判断是否已存在管理员。
func (s *Store) CountPlatformAdmins(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM users WHERE is_platform_admin = TRUE AND status = 'active'`,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count platform admins: %w", err)
	}
	return n, nil
}

// SetUserStatus 启用/禁用用户。
func (s *Store) SetUserStatus(ctx context.Context, id int64, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ? WHERE id = ?`, status, id)
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

// SetPlatformAdmin 调整平台管理员标记。
func (s *Store) SetPlatformAdmin(ctx context.Context, id int64, admin bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET is_platform_admin = ? WHERE id = ?`, admin, id)
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
