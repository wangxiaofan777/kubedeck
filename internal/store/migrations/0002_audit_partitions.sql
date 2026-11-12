-- 审计月分区：预创建 + 兜底分区 + 保留期清理。
--
-- 为什么不靠 BEFORE INSERT 触发器按需建分区：分区表的元组路由发生在行级触发器之前，
-- 目标分区不存在时插入会在触发器执行前失败。因此采用“提前创建若干月分区 + DEFAULT 兜底”，
-- 由启动时的维护任务按月补齐（tasks 2.3）。

CREATE OR REPLACE FUNCTION audit_partition_name(ts TIMESTAMPTZ) RETURNS TEXT AS $$
    SELECT 'audit_logs_' || to_char(date_trunc('month', ts AT TIME ZONE 'UTC'), 'YYYYMM');
$$ LANGUAGE sql IMMUTABLE;

-- 创建 ts 所在月份的分区；已存在则什么也不做。并发调用由 advisory lock 串行化。
CREATE OR REPLACE FUNCTION ensure_audit_partition(ts TIMESTAMPTZ) RETURNS TEXT AS $$
DECLARE
    start_ts  TIMESTAMPTZ := date_trunc('month', ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';
    end_ts    TIMESTAMPTZ := (date_trunc('month', ts AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC';
    part_name TEXT := audit_partition_name(ts);
BEGIN
    IF to_regclass(part_name) IS NOT NULL THEN
        RETURN part_name;
    END IF;

    PERFORM pg_advisory_xact_lock(hashtext(part_name));
    IF to_regclass(part_name) IS NULL THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF audit_logs FOR VALUES FROM (%L) TO (%L)',
            part_name, start_ts, end_ts
        );
    END IF;
    RETURN part_name;
END;
$$ LANGUAGE plpgsql;

-- 维护入口：保证 [上个月, 当前月 + ahead_months] 的分区都存在。
-- 调用方在服务启动时与每日定时任务中各执行一次。
CREATE OR REPLACE FUNCTION ensure_audit_partitions(ahead_months INT DEFAULT 3) RETURNS INT AS $$
DECLARE
    created INT := 0;
    i       INT;
    target  TIMESTAMPTZ;
    before  INT;
BEGIN
    IF ahead_months < 0 THEN
        RAISE EXCEPTION 'ahead_months must be >= 0, got %', ahead_months;
    END IF;

    FOR i IN -1..ahead_months LOOP
        target := now() + (i || ' month')::interval;
        PERFORM ensure_audit_partition(target);
        created := created + 1;
    END LOOP;
    RETURN created;
END;
$$ LANGUAGE plpgsql;

-- 保留策略：删除整月分区，返回被删除的分区名。
-- 只删除名称符合 audit_logs_YYYYMM 且整月早于 now()-retain_days 的分区；DEFAULT 分区不动。
CREATE OR REPLACE FUNCTION drop_expired_audit_partitions(retain_days INT) RETURNS SETOF TEXT AS $$
DECLARE
    part        RECORD;
    suffix      TEXT;
    upper_bound TIMESTAMPTZ;
    cutoff      TIMESTAMPTZ := now() - make_interval(days => retain_days);
BEGIN
    IF retain_days < 1 THEN
        RAISE EXCEPTION 'retain_days must be >= 1, got %', retain_days;
    END IF;

    FOR part IN
        SELECT c.relname
        FROM pg_class c
        JOIN pg_inherits i ON i.inhrelid = c.oid
        JOIN pg_class p ON p.oid = i.inhparent
        WHERE p.relname = 'audit_logs'
          AND c.relname ~ '^audit_logs_[0-9]{6}$'
        ORDER BY c.relname
    LOOP
        suffix := substr(part.relname, length('audit_logs_') + 1);
        upper_bound := (to_date(suffix || '01', 'YYYYMMDD') + interval '1 month') AT TIME ZONE 'UTC';
        IF upper_bound <= cutoff THEN
            EXECUTE format('DROP TABLE %I', part.relname);
            RETURN NEXT part.relname;
        END IF;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- 兜底分区：任何未预创建的月份都会落到这里，避免审计写入失败。
CREATE TABLE IF NOT EXISTS audit_logs_default PARTITION OF audit_logs DEFAULT;

SELECT ensure_audit_partitions(3);