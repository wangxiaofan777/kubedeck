package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const bindingSelect = `
	SELECT rb.id, rb.user_id, rb.cluster_id, c.name, rb.namespace, r.code, rb.created_at
	FROM role_bindings rb
	JOIN clusters c ON c.id = rb.cluster_id
	JOIN roles r ON r.id = rb.role_id`

func scanBinding(row pgx.Row) (domain.RoleBinding, error) {
	var b domain.RoleBinding
	err := row.Scan(&b.ID, &b.UserID, &b.ClusterID, &b.ClusterName,
		&b.Namespace, &b.RoleCode, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleBinding{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.RoleBinding{}, fmt.Errorf("store: scan role binding: %w", err)
	}
	return b, nil
}

// CreateRoleBinding 创建（或幂等刷新）绑定。namespace 为空时视为全部命名空间 "*"。
// 重复绑定同一四元组不会报错，也不会产生重复行。
func (s *Store) CreateRoleBinding(ctx context.Context, userID, clusterID int64, namespace, roleCode string, grantedBy *int64) (domain.RoleBinding, error) {
	if namespace == "" {
		namespace = "*"
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO role_bindings (user_id, cluster_id, namespace, role_id, granted_by)
		SELECT $1, $2, $3, r.id, $5 FROM roles r WHERE r.code = $4
		ON CONFLICT (user_id, cluster_id, namespace, role_id)
		DO UPDATE SET granted_by = EXCLUDED.granted_by
		RETURNING id`, userID, clusterID, namespace, roleCode, grantedBy).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleBinding{}, fmt.Errorf("%w: role %q", domain.ErrNotFound, roleCode)
	}
	if err != nil {
		return domain.RoleBinding{}, mapError(err)
	}
	return s.RoleBindingByID(ctx, id)
}

// RoleBindingByID 读取单条绑定。
func (s *Store) RoleBindingByID(ctx context.Context, id int64) (domain.RoleBinding, error) {
	b, err := scanBinding(s.pool.QueryRow(ctx, bindingSelect+` WHERE rb.id = $1`, id))
	return b, mapError(err)
}

// BindingsForUser 返回用户全部绑定，用于登录后计算可访问集群与命名空间。
func (s *Store) BindingsForUser(ctx context.Context, userID int64) ([]domain.RoleBinding, error) {
	return s.queryBindings(ctx, bindingSelect+` WHERE rb.user_id = $1 ORDER BY c.name, rb.namespace`, userID)
}

// BindingsForCluster 返回集群上的全部绑定，用于权限管理与审计。
func (s *Store) BindingsForCluster(ctx context.Context, clusterID int64) ([]domain.RoleBinding, error) {
	return s.queryBindings(ctx, bindingSelect+` WHERE rb.cluster_id = $1 ORDER BY rb.user_id, rb.namespace`, clusterID)
}

func (s *Store) queryBindings(ctx context.Context, sql string, args ...any) ([]domain.RoleBinding, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list role bindings: %w", err)
	}
	defer rows.Close()

	bindings := make([]domain.RoleBinding, 0)
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, b)
	}
	return bindings, mapError(rows.Err())
}

// DeleteRoleBinding 回收绑定；不存在时返回 domain.ErrNotFound。
func (s *Store) DeleteRoleBinding(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM role_bindings WHERE id = $1`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RoleBindingInput 描述一条待写入的绑定；Namespace 为空表示全部命名空间。
type RoleBindingInput = domain.RoleBindingInput

// SetClusterAccess 用给定集合整体覆盖某用户在某个集群上的绑定：
// 先删除 user×cluster 的旧绑定，再写入新绑定，保证一次事务内生效。
// 输入中的重复四元组会被去重，避免触发唯一约束导致整笔回滚。
func (s *Store) SetClusterAccess(ctx context.Context, userID, clusterID int64, bindings []RoleBindingInput, grantedBy *int64) error {
	normalized := make([]RoleBindingInput, 0, len(bindings))
	seen := make(map[RoleBindingInput]struct{}, len(bindings))
	for _, b := range bindings {
		if b.RoleCode == "" {
			return errors.New("store: role code must not be empty")
		}
		ns := b.Namespace
		if ns == "" {
			ns = "*"
		}
		item := RoleBindingInput{Namespace: ns, RoleCode: b.RoleCode}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin set cluster access: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM role_bindings WHERE user_id = $1 AND cluster_id = $2`, userID, clusterID); err != nil {
		return mapError(err)
	}
	for _, b := range normalized {
		tag, err := tx.Exec(ctx, `
			INSERT INTO role_bindings (user_id, cluster_id, namespace, role_id, granted_by)
			SELECT $1, $2, $3, r.id, $5 FROM roles r WHERE r.code = $4`,
			userID, clusterID, b.Namespace, b.RoleCode, grantedBy)
		if err != nil {
			return mapError(err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: role %q", domain.ErrNotFound, b.RoleCode)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit set cluster access: %w", err)
	}
	return nil
}
