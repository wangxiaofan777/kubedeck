package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const bindingSelect = `
	SELECT rb.id, rb.user_id, rb.cluster_id, c.name, rb.namespace, r.code, rb.created_at
	FROM role_bindings rb
	JOIN clusters c ON c.id = rb.cluster_id
	JOIN roles r ON r.id = rb.role_id`

// RoleBindingInput 描述一条待写入的绑定；Namespace 为空表示全部命名空间。
type RoleBindingInput = domain.RoleBindingInput

func scanBinding(row rowScanner) (domain.RoleBinding, error) {
	var b domain.RoleBinding
	err := row.Scan(&b.ID, &b.UserID, &b.ClusterID, &b.ClusterName,
		&b.Namespace, &b.RoleCode, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.RoleBinding{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.RoleBinding{}, fmt.Errorf("store: scan role binding: %w", err)
	}
	return b, nil
}

// CreateRoleBinding 创建（或幂等刷新）绑定。namespace 为空时视为全部命名空间 "*"。
func (s *Store) CreateRoleBinding(ctx context.Context, userID, clusterID int64, namespace, roleCode string, grantedBy *int64) (domain.RoleBinding, error) {
	if namespace == "" {
		namespace = "*"
	}
	roleID, err := s.roleID(ctx, s.db, roleCode)
	if err != nil {
		return domain.RoleBinding{}, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO role_bindings (user_id, cluster_id, namespace, role_id, granted_by)
		VALUES (?, ?, ?, ?, ?) AS new
		ON DUPLICATE KEY UPDATE
			id = LAST_INSERT_ID(id),
			granted_by = new.granted_by`,
		userID, clusterID, namespace, roleID, grantedBy)
	if err != nil {
		return domain.RoleBinding{}, mapError(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.RoleBinding{}, fmt.Errorf("store: create role binding id: %w", err)
	}
	return s.RoleBindingByID(ctx, id)
}

// RoleBindingByID 读取单条绑定。
func (s *Store) RoleBindingByID(ctx context.Context, id int64) (domain.RoleBinding, error) {
	b, err := scanBinding(s.db.QueryRowContext(ctx, bindingSelect+` WHERE rb.id = ?`, id))
	return b, mapError(err)
}

// BindingsForUser 返回用户全部绑定，用于登录后计算可访问集群与命名空间。
func (s *Store) BindingsForUser(ctx context.Context, userID int64) ([]domain.RoleBinding, error) {
	return s.queryBindings(ctx, bindingSelect+` WHERE rb.user_id = ? ORDER BY c.name, rb.namespace`, userID)
}

// BindingsForCluster 返回集群上的全部绑定，用于权限管理与审计。
func (s *Store) BindingsForCluster(ctx context.Context, clusterID int64) ([]domain.RoleBinding, error) {
	return s.queryBindings(ctx, bindingSelect+` WHERE rb.cluster_id = ? ORDER BY rb.user_id, rb.namespace`, clusterID)
}

func (s *Store) queryBindings(ctx context.Context, query string, args ...any) ([]domain.RoleBinding, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	res, err := s.db.ExecContext(ctx, `DELETE FROM role_bindings WHERE id = ?`, id)
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

// SetClusterAccess 用给定集合整体覆盖某用户在某个集群上的绑定。
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin set cluster access: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM role_bindings WHERE user_id = ? AND cluster_id = ?`, userID, clusterID); err != nil {
		return mapError(err)
	}
	for _, b := range normalized {
		roleID, err := s.roleID(ctx, tx, b.RoleCode)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO role_bindings (user_id, cluster_id, namespace, role_id, granted_by)
			VALUES (?, ?, ?, ?, ?)`,
			userID, clusterID, b.Namespace, roleID, grantedBy); err != nil {
			return mapError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit set cluster access: %w", err)
	}
	return nil
}

// roleID 查询角色主键；缺失时返回带角色代码的 ErrNotFound。
func (s *Store) roleID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, code string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM roles WHERE code = ?`, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: role %q", domain.ErrNotFound, code)
	}
	if err != nil {
		return 0, fmt.Errorf("store: lookup role %q: %w", code, err)
	}
	return id, nil
}
