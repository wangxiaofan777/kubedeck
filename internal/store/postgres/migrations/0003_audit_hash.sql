-- 审计哈希链摘要函数。
--
-- 为什么放在 SQL 里：detail 是 JSONB，PostgreSQL 会规范化键顺序与数字格式。
-- 若在 Go 侧对原始字节做哈希，读回后重新序列化得到的字节可能不同，链校验会误报。
-- 统一由数据库对写入值与读取值做同一套规范化，保证「写时算的哈希」与「读时算的哈希」一致。

CREATE OR REPLACE FUNCTION audit_log_hash(
    p_occurred_at   TIMESTAMPTZ,
    p_request_id    TEXT,
    p_actor_id      BIGINT,
    p_actor_subject TEXT,
    p_actor_email   TEXT,
    p_cluster_id    BIGINT,
    p_cluster_name  TEXT,
    p_verb          TEXT,
    p_api_group     TEXT,
    p_resource      TEXT,
    p_namespace     TEXT,
    p_name          TEXT,
    p_result        TEXT,
    p_result_code   INT,
    p_latency_ms    INT,
    p_client_ip     INET,
    p_user_agent    TEXT,
    p_body_hash     TEXT,
    p_detail        JSONB,
    p_prev_hash     TEXT
) RETURNS TEXT AS $$
    SELECT encode(sha256(convert_to(
        concat_ws(E'\x1f',
            to_char(p_occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'),
            COALESCE(p_request_id, ''),
            COALESCE(p_actor_id::text, ''),
            COALESCE(p_actor_subject, ''),
            COALESCE(p_actor_email, ''),
            COALESCE(p_cluster_id::text, ''),
            COALESCE(p_cluster_name, ''),
            COALESCE(p_verb, ''),
            COALESCE(p_api_group, ''),
            COALESCE(p_resource, ''),
            COALESCE(p_namespace, ''),
            COALESCE(p_name, ''),
            COALESCE(p_result, ''),
            COALESCE(p_result_code::text, ''),
            COALESCE(p_latency_ms::text, ''),
            COALESCE(p_client_ip::text, ''),
            COALESCE(p_user_agent, ''),
            COALESCE(p_body_hash, ''),
            COALESCE(p_detail, '{}'::jsonb)::text,
            COALESCE(p_prev_hash, '')
        ), 'UTF8')), 'hex');
$$ LANGUAGE sql IMMUTABLE;