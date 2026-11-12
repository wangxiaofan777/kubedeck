-- 平台元数据表。集群资源状态一律不入库（design D6）。
-- 时间统一以 UTC 存储（timestamptz）。

CREATE TABLE users (
    id                BIGSERIAL PRIMARY KEY,
    subject           TEXT NOT NULL UNIQUE,             -- OIDC sub
    email             TEXT NOT NULL DEFAULT '',
    display_name      TEXT NOT NULL DEFAULT '',
    source            TEXT NOT NULL DEFAULT 'oidc' CHECK (source IN ('oidc', 'local')),
    is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at     TIMESTAMPTZ
);

CREATE TABLE clusters (
    id             BIGSERIAL PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    display_name   TEXT NOT NULL DEFAULT '',
    server         TEXT NOT NULL,
    transport      TEXT NOT NULL DEFAULT 'direct' CHECK (transport IN ('direct', 'agent')),
    auth_mode      TEXT NOT NULL DEFAULT 'impersonation'
                   CHECK (auth_mode IN ('impersonation', 'token-passthrough')),
    server_version TEXT NOT NULL DEFAULT '',            -- 接入探测与健康检查刷新（D12）
    status         TEXT NOT NULL DEFAULT 'unknown'
                   CHECK (status IN ('healthy', 'degraded', 'unreachable', 'unknown')),
    message        TEXT NOT NULL DEFAULT '',
    labels         JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at   TIMESTAMPTZ,
    created_by     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX clusters_status_idx ON clusters (status);

-- 每个集群一条凭据；轮换时原地覆盖，旧凭据立即失效（cluster-registry 凭据轮换场景）。
CREATE TABLE cluster_credentials (
    cluster_id  BIGINT PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    ciphertext  BYTEA NOT NULL,
    nonce       BYTEA NOT NULL,
    key_version INT NOT NULL,
    auth_type   TEXT NOT NULL CHECK (auth_type IN ('kubeconfig', 'token', 'client-cert')),
    fingerprint TEXT NOT NULL,
    rotated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ
);

CREATE INDEX cluster_credentials_key_version_idx ON cluster_credentials (key_version);

-- 平台角色（预置，不可删除）。permissions 只描述平台级能力，不复制 K8S 权限规则。
CREATE TABLE roles (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    permissions JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_system   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO roles (code, name, description, permissions) VALUES
    ('platform_admin', 'PlatformAdmin', '管理集群、用户与权限，可读全部审计',
     '{"clusters":"manage","users":"manage","role_bindings":"manage","audit":"read_all"}'),
    ('cluster_admin', 'ClusterAdmin', '单集群全命名空间读写，可读本集群审计',
     '{"clusters":"read","audit":"read_cluster"}'),
    ('developer', 'Developer', '指定命名空间读写', '{}'),
    ('viewer', 'Viewer', '指定命名空间只读', '{}');

-- 绑定：用户 × 集群 × 命名空间 × 角色（四元组唯一）。namespace='*' 表示全部命名空间。
CREATE TABLE role_bindings (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace  TEXT NOT NULL DEFAULT '*',
    role_id    BIGINT NOT NULL REFERENCES roles(id),
    granted_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, cluster_id, namespace, role_id)
);

CREATE INDEX role_bindings_user_cluster_idx ON role_bindings (user_id, cluster_id);

-- 操作审计：按月分区、只追加、哈希链（audit-log 规格）。
-- 只读列表浏览不入本表，请求元数据走访问日志。
CREATE TABLE audit_logs (
    id            BIGSERIAL,
    occurred_at   TIMESTAMPTZ NOT NULL,
    request_id    TEXT NOT NULL DEFAULT '',             -- 请求追踪号
    actor_id      BIGINT,
    actor_subject TEXT NOT NULL,
    actor_email   TEXT NOT NULL DEFAULT '',
    cluster_id    BIGINT,
    cluster_name  TEXT NOT NULL DEFAULT '',
    verb          TEXT NOT NULL,
    api_group     TEXT NOT NULL DEFAULT '',
    resource      TEXT NOT NULL DEFAULT '',
    namespace     TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL DEFAULT 'success' CHECK (result IN ('success', 'failure')),
    result_code   INT NOT NULL DEFAULT 0,
    latency_ms    INT NOT NULL DEFAULT 0,
    client_ip     INET,
    user_agent    TEXT NOT NULL DEFAULT '',
    body_hash     TEXT NOT NULL DEFAULT '',             -- 请求体摘要，绝不存明文
    detail        JSONB NOT NULL DEFAULT '{}'::jsonb,    -- 命令、diff 摘要、dryRun 等
    prev_hash     TEXT NOT NULL DEFAULT '',
    hash          TEXT NOT NULL,
    PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);

-- 审计只允许追加：数据库层拒绝 UPDATE/DELETE（保留策略通过 DROP PARTITION 实现）。
CREATE OR REPLACE FUNCTION audit_logs_reject_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs is append-only: % is not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_logs_append_only
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_reject_mutation();

-- 操作模板（后续能力使用）
CREATE TABLE operation_templates (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    scope      TEXT NOT NULL CHECK (scope IN ('platform', 'cluster', 'namespace')),
    payload    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value) VALUES
    ('audit.retention_days', '180'::jsonb),
    ('platform.min_k8s_version', '"1.34"'::jsonb);