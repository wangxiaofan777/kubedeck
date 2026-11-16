package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kubedeck/kubedeck/internal/domain"
)

// GetSetting 读取平台配置项，返回原始 JSON 值。
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE `key` = ?", key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
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
	if _, err := s.db.ExecContext(ctx, "INSERT INTO app_settings (`key`, value) "+
		"VALUES (?, ?) AS new "+
		"ON DUPLICATE KEY UPDATE value = new.value, updated_at = UTC_TIMESTAMP(6)",
		key, string(raw)); err != nil {
		return mapError(err)
	}
	return nil
}
