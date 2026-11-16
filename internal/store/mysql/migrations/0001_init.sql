-- 平台元数据表（MySQL 8）。集群资源状态一律不入库。
-- 时间统一使用 UTC 存储（DATETIME(6)），连接串强制 time_zone='+00:00'。
-- 本文件内的语句全部幂等，允许 DDL 隐式提交后重跑。

CREATE TABLE IF NOT EXISTS users (
    id                BIGINT NOT NULL AUTO_INCREMENT,
    subject           VARCHAR(255) NOT NULL,
    email             VARCHAR(320) NOT NULL DEFAULT '',
    display_name      VARCHAR(255) NOT NULL DEFAULT '',
    source            VARCHAR(16) NOT NULL DEFAULT 'oidc',
    is_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    status            VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at        DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_login_at     DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY users_subject_key (subject),
    CONSTRAINT users_source_check CHECK (source IN ('oidc', 'local')),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS clusters (
    id             BIGINT NOT NULL AUTO_INCREMENT,
    name           VARCHAR(253) NOT NULL,
    display_name   VARCHAR(255) NOT NULL DEFAULT '',
    server         VARCHAR(1024) NOT NULL,
    transport      VARCHAR(32) NOT NULL DEFAULT 'direct',
    auth_mode      VARCHAR(32) NOT NULL DEFAULT 'impersonation',
    server_version VARCHAR(64) NOT NULL DEFAULT '',
    status         VARCHAR(32) NOT NULL DEFAULT 'unknown',
    message        TEXT NOT NULL,
    labels         JSON NOT NULL,
    last_seen_at   DATETIME(6) NULL,
    created_by     BIGINT NULL,
    created_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at     DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY clusters_name_key (name),
    KEY clusters_status_idx (status),
    CONSTRAINT clusters_transport_check CHECK (transport IN ('direct', 'agent')),
    CONSTRAINT clusters_auth_mode_check CHECK (auth_mode IN ('impersonation', 'token-passthrough')),
    CONSTRAINT clusters_status_check CHECK (status IN ('healthy', 'degraded', 'unreachable', 'unknown')),
    CONSTRAINT clusters_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 每个集群一条凭据；轮换时原地覆盖，旧凭据立即失效。
CREATE TABLE IF NOT EXISTS cluster_credentials (
    cluster_id  BIGINT NOT NULL,
    ciphertext  MEDIUMBLOB NOT NULL,
    nonce       VARBINARY(64) NOT NULL,
    key_version INT NOT NULL,
    auth_type   VARCHAR(32) NOT NULL,
    fingerprint VARCHAR(255) NOT NULL,
    rotated_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at  DATETIME(6) NULL,
    PRIMARY KEY (cluster_id),
    KEY cluster_credentials_key_version_idx (key_version),
    CONSTRAINT cluster_credentials_auth_type_check CHECK (auth_type IN ('kubeconfig', 'token', 'client-cert')),
    CONSTRAINT cluster_credentials_cluster_fk FOREIGN KEY (cluster_id) REFERENCES clusters (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 平台角色（预置，不可删除）。
CREATE TABLE IF NOT EXISTS roles (
    id          BIGINT NOT NULL AUTO_INCREMENT,
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    permissions JSON NOT NULL,
    is_system   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY roles_code_key (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO roles (code, name, description, permissions, is_system) VALUES
    ('platform_admin', 'PlatformAdmin', '管理集群、用户与权限，可读全部审计',
     '{"clusters":"manage","users":"manage","role_bindings":"manage","audit":"read_all"}', TRUE),
    ('cluster_admin', 'ClusterAdmin', '单集群全命名空间读写，可读本集群审计',
     '{"clusters":"read","audit":"read_cluster"}', TRUE),
    ('developer', 'Developer', '指定命名空间读写', '{}', TRUE),
    ('viewer', 'Viewer', '指定命名空间只读', '{}', TRUE);

CREATE TABLE IF NOT EXISTS role_bindings (
    id         BIGINT NOT NULL AUTO_INCREMENT,
    user_id    BIGINT NOT NULL,
    cluster_id BIGINT NOT NULL,
    namespace  VARCHAR(253) NOT NULL DEFAULT '*',
    role_id    BIGINT NOT NULL,
    granted_by BIGINT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY role_bindings_quad_key (user_id, cluster_id, namespace, role_id),
    KEY role_bindings_user_cluster_idx (user_id, cluster_id),
    CONSTRAINT role_bindings_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT role_bindings_cluster_fk FOREIGN KEY (cluster_id) REFERENCES clusters (id) ON DELETE CASCADE,
    CONSTRAINT role_bindings_role_fk FOREIGN KEY (role_id) REFERENCES roles (id),
    CONSTRAINT role_bindings_granted_by_fk FOREIGN KEY (granted_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 操作审计：按月分区、只追加、哈希链。
-- 初始只有 MAXVALUE 兜底分区；启动/每日维护会把未来月份拆分出来。
CREATE TABLE IF NOT EXISTS audit_logs (
    id            BIGINT NOT NULL AUTO_INCREMENT,
    occurred_at   DATETIME(6) NOT NULL,
    request_id    VARCHAR(191) NOT NULL DEFAULT '',
    actor_id      BIGINT NULL,
    actor_subject VARCHAR(255) NOT NULL,
    actor_email   VARCHAR(320) NOT NULL DEFAULT '',
    cluster_id    BIGINT NULL,
    cluster_name  VARCHAR(253) NOT NULL DEFAULT '',
    verb          VARCHAR(64) NOT NULL,
    api_group     VARCHAR(255) NOT NULL DEFAULT '',
    resource      VARCHAR(128) NOT NULL DEFAULT '',
    namespace     VARCHAR(253) NOT NULL DEFAULT '',
    name          VARCHAR(253) NOT NULL DEFAULT '',
    result        VARCHAR(16) NOT NULL DEFAULT 'success',
    result_code   INT NOT NULL DEFAULT 0,
    latency_ms    INT NOT NULL DEFAULT 0,
    client_ip     VARCHAR(45) NULL,
    user_agent    VARCHAR(1024) NOT NULL DEFAULT '',
    body_hash     VARCHAR(128) NOT NULL DEFAULT '',
    detail        JSON NOT NULL,
    prev_hash     VARCHAR(64) NOT NULL DEFAULT '',
    hash          VARCHAR(64) NOT NULL,
    PRIMARY KEY (id, occurred_at),
    KEY audit_logs_occurred_at_idx (occurred_at),
    KEY audit_logs_actor_occurred_idx (actor_subject, occurred_at),
    KEY audit_logs_cluster_occurred_idx (cluster_name, occurred_at),
    KEY audit_logs_result_occurred_idx (result, occurred_at),
    CONSTRAINT audit_logs_result_check CHECK (result IN ('success', 'failure'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
PARTITION BY RANGE COLUMNS (occurred_at) (
    PARTITION audit_logs_max VALUES LESS THAN (MAXVALUE)
);

-- 只追加：UPDATE/DELETE 由触发器拒绝；保留期通过 DROP PARTITION 实现。
DROP TRIGGER IF EXISTS audit_logs_no_update;
CREATE TRIGGER audit_logs_no_update BEFORE UPDATE ON audit_logs
FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs is append-only: UPDATE is not allowed';

DROP TRIGGER IF EXISTS audit_logs_no_delete;
CREATE TRIGGER audit_logs_no_delete BEFORE DELETE ON audit_logs
FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_logs is append-only: DELETE is not allowed';

-- 跨进程事务锁：审计链追加与首个管理员引导。
CREATE TABLE IF NOT EXISTS store_locks (
    name       VARCHAR(64) NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO store_locks (name) VALUES ('audit_chain'), ('bootstrap');

CREATE TABLE IF NOT EXISTS operation_templates (
    id         BIGINT NOT NULL AUTO_INCREMENT,
    name       VARCHAR(255) NOT NULL,
    scope      VARCHAR(16) NOT NULL,
    payload    JSON NOT NULL,
    created_by BIGINT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT operation_templates_scope_check CHECK (scope IN ('platform', 'cluster', 'namespace')),
    CONSTRAINT operation_templates_created_by_fk FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS app_settings (
    `key`      VARCHAR(128) NOT NULL,
    value      JSON NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO app_settings (`key`, value) VALUES
    ('audit.retention_days', '180'),
    ('platform.min_k8s_version', '"1.34"');