package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// bootstrapLockKey 串行化首个管理员创建，避免并发引导产生两个管理员。
const bootstrapLockKey int64 = 0x626f6f74737472 // "bootstr"

// BootstrapAdmin 创建（或提升）首个平台管理员。
// 已存在管理员时返回 domain.ErrConflict，不修改任何账号。
func (s *Store) BootstrapAdmin(ctx context.Context, subject, email, displayName string) (domain.User, error) {
	if subject == "" {
		return domain.User{}, fmt.Errorf("%w: subject is required", domain.ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("store: begin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, bootstrapLockKey); err != nil {
		return domain.User{}, fmt.Errorf("store: lock bootstrap: %w", err)
	}
	var admins int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM users WHERE is_platform_admin AND status = 'active'`).Scan(&admins); err != nil {
		return domain.User{}, fmt.Errorf("store: count bootstrap admins: %w", err)
	}
	if admins > 0 {
		return domain.User{}, domain.ErrConflict
	}

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO users (subject, email, display_name, source, is_platform_admin, last_login_at)
		VALUES ($1, $2, $3, 'local', TRUE, now())
		ON CONFLICT (subject) DO UPDATE
		SET email = COALESCE(NULLIF(EXCLUDED.email, ''), users.email),
		    display_name = COALESCE(NULLIF(EXCLUDED.display_name, ''), users.display_name),
		    is_platform_admin = TRUE,
		    status = 'active'
		RETURNING id`, subject, email, displayName).Scan(&id)
	if err != nil {
		return domain.User{}, mapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, fmt.Errorf("store: commit bootstrap: %w", err)
	}
	return s.UserByID(ctx, id)
}

// SearchUsers 按 subject/email/display_name 模糊查询，供权限管理界面选择用户。
func (s *Store) SearchUsers(ctx context.Context, query string, limit int) ([]domain.User, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT `+userColumns+` FROM users
		WHERE lower(subject) LIKE $1 OR lower(email) LIKE $1 OR lower(display_name) LIKE $1
		ORDER BY display_name, id
		LIMIT $2`, q, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search users: %w", err)
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

// AllRoleBindings 返回全部绑定，按集群、用户、命名空间排序，供管理界面与审计使用。
func (s *Store) AllRoleBindings(ctx context.Context) ([]domain.RoleBinding, error) {
	return s.queryBindings(ctx, bindingSelect+` ORDER BY c.name, rb.user_id, rb.namespace`)
}
