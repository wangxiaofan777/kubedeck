package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// Roles 返回平台预置角色。
func (s *Store) Roles(ctx context.Context) ([]domain.Role, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, description, permissions, is_system, created_at
		FROM roles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list roles: %w", err)
	}
	defer rows.Close()

	roles := make([]domain.Role, 0)
	for rows.Next() {
		var (
			r    domain.Role
			perm []byte
		)
		if err := rows.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &perm, &r.IsSystem, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan role: %w", err)
		}
		if len(perm) > 0 {
			if err := json.Unmarshal(perm, &r.Permissions); err != nil {
				return nil, fmt.Errorf("store: decode role permissions: %w", err)
			}
		}
		roles = append(roles, r)
	}
	return roles, mapError(rows.Err())
}

// RoleByCode 按角色代码查询。
func (s *Store) RoleByCode(ctx context.Context, code string) (domain.Role, error) {
	var (
		r    domain.Role
		perm []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, name, description, permissions, is_system, created_at
		FROM roles WHERE code = $1`, code,
	).Scan(&r.ID, &r.Code, &r.Name, &r.Description, &perm, &r.IsSystem, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Role{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Role{}, fmt.Errorf("store: scan role: %w", err)
	}
	if len(perm) > 0 {
		if err := json.Unmarshal(perm, &r.Permissions); err != nil {
			return domain.Role{}, fmt.Errorf("store: decode role permissions: %w", err)
		}
	}
	return r, nil
}
