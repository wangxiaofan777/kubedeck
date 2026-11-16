-- 最低支持版本从旧默认 1.34 调整为 1.18（legacy 兼容档）。
-- 仅当值仍等于旧默认时更新，避免覆盖管理员已自定义的最低版本。
UPDATE app_settings
SET value = '"1.18"'::jsonb
WHERE key = 'platform.min_k8s_version'
  AND value = '"1.34"'::jsonb;