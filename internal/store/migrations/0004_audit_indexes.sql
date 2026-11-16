-- 审计查询与统计的常用过滤条件索引。
-- audit_logs 按月分区，索引会自动应用到各分区。
CREATE INDEX IF NOT EXISTS audit_logs_occurred_at_idx
    ON audit_logs (occurred_at);

CREATE INDEX IF NOT EXISTS audit_logs_actor_occurred_idx
    ON audit_logs (actor_subject, occurred_at DESC);

CREATE INDEX IF NOT EXISTS audit_logs_cluster_occurred_idx
    ON audit_logs (cluster_name, occurred_at DESC);

CREATE INDEX IF NOT EXISTS audit_logs_result_occurred_idx
    ON audit_logs (result, occurred_at DESC);