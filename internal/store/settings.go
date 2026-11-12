package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// GetSetting 读取平台配置项，返回原始 JSON 值。
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: setting %q", domain.ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return raw, nil
}

// SetSetting 写入平台配置项（键值 upsert）。
func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("store: encode setting %q: %w", key, err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = now()`, key, raw); err != nil {
		return mapError(err)
	}
	return nil
}
