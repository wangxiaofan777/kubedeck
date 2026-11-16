## Context

现有存储层把 pgx、PostgreSQL SQL 方言、plpgsql 哈希函数、advisory lock 与月分区直接写在 `internal/store` 中，业务层通过若干窄接口（`audit.Store`、`api.AuditStore/AuthStore/AdminStore`、`auth.UserStore`、`cluster.Store`）使用仓储。迁移采用嵌入 SQL + 校验和，在同一事务内应用。

约束：

- PostgreSQL 现有部署必须零行为变化，已有审计链升级后仍可校验。
- MySQL 8 的 DDL 隐式提交、无 `RETURNING`、`GET_LOCK` 会话级锁、JSON 规范化与 PG 不同。
- 项目使用 OpenSpec 规格驱动，测试必须连真实数据库。

## Goals / Non-Goals

**Goals:**

- 一个仓储接口，两套实现，业务层不感知引擎。
- 两引擎在认证、授权、集群注册、凭据轮换、设置、审计查询/统计上行为等价。
- MySQL 上保留审计只追加、哈希链、月分区与保留期清理。
- 同一套集成测试跑两种真实数据库；部署面可选 MySQL。

**Non-Goals:**

- 不提供 PG → MySQL 数据搬迁，不保证跨引擎哈希链连续。
- 不把会话搬到数据库或 Redis。
- 不为未知引擎做插件加载或泛化 SQL 方言层。

## Decisions

### D1. 引擎选择：连接串 + 显式驱动

`KUBEDECK_DATABASE_URL` 仍是唯一连接串；新增 `KUBEDECK_DB_DRIVER`（`postgres`/`mysql`）可选。

- 驱动为空：按 URL scheme 识别（`postgres://`/`postgresql://`/`mysql://`）；无 scheme 时回退 postgres，保持既有 PG keyword DSN 兼容。
- 驱动显式：与可识别 scheme 冲突时报错；不支持的取值在配置校验阶段失败。
- 替代方案：新增 host/port/user 等分项配置。放弃，因为会破坏现有单一连接串契约，并让 Helm/Compose 出现两套变量。

### D2. 包结构：接口根包 + 每引擎子包

```
internal/store/           Store 接口、共享 DTO、驱动解析、分页/审计哈希工具
internal/store/postgres/  现有实现迁入，SQL/迁移不变
internal/store/mysql/     database/sql + go-sql-driver/mysql
internal/store/storetest/ 参数化一致性测试套件
```

业务层已有的窄接口保持不变；`app` 依赖 `store.Store`。新增引擎的固定步骤：实现接口、提供嵌入迁移、在 `app.openDatabase` 注册、复用一致性测试。

替代方案：单一实现 + Dialect 接口拼接 SQL。放弃，因为 upsert、`RETURNING`、分区、哈希函数、锁的差异会让 Dialect 变成大量条件分支，抽象反而更脆。

### D3. PG 实现保持原样

SQL、plpgsql 函数、触发器、advisory lock、分区函数与审计哈希全部保留，只做包迁移和接口适配。`mapError` 留在 PG 包内。这样既避免回归，也保证现有审计链不被重算。

### D4. MySQL 数据映射

| 概念 | PG | MySQL |
|---|---|---|
| 自增主键 | `BIGSERIAL` | `BIGINT AUTO_INCREMENT` |
| 时间 | `TIMESTAMPTZ` | `DATETIME(6)`，会话时区固定 UTC |
| JSON | `JSONB` | `JSON` |
| 二进制 | `BYTEA` | `MEDIUMBLOB` |
| IP | `INET` | `VARCHAR(45)` |
| 布尔 | `BOOLEAN` | `BOOLEAN`（`TINYINT(1)`），谓词显式 `= TRUE` |

被索引字符串使用 `VARCHAR`（`subject`、集群名、角色 code、设置 key 等），避免 MySQL TEXT 前缀索引。时间统一由应用传 UTC，DSN 强制 `parseTime=true&loc=UTC&time_zone='+00:00'`。

### D5. 审计哈希按引擎实现

PG 继续在 SQL 内计算；MySQL 在 Go 侧规范化：

1. 解析 `detail` 为 `any`（`json.Decoder.UseNumber()`），再 `json.Marshal`，得到键排序稳定、无多余空白的规范文本。
2. 按与 PG 相同的字段顺序和 `\x1f` 分隔拼接，时间格式 `2006-01-02T15:04:05.000000`（UTC），SHA-256 后取十六进制。

读写都走同一函数，因此 MySQL 内稳定；PG 与 MySQL 的链互不校验，文档与规格明确该边界。替代方案：在 MySQL 用 `SHA2`/`JSON_COMPACT` 复刻 PG 函数。放弃，因为 MySQL JSON 规范化行为与 PG 不同，且 SQL 版本受版本/字符集影响。

### D6. MySQL 只追加与分区

- 只追加：`BEFORE UPDATE` / `BEFORE DELETE` 触发器 `SIGNAL SQLSTATE '45000'`。保留期删除使用 `DROP PARTITION`，不触发触发器，与 PG 的 `DROP TABLE 分区` 语义一致。
- 分区：`PARTITION BY RANGE COLUMNS(occurred_at)`，初始只有 `audit_logs_max VALUES LESS THAN (MAXVALUE)` 兜底。维护时按 `current-1..current+3` 计算边界，对缺失月份执行 `REORGANIZE PARTITION audit_logs_max INTO (PARTITION p_YYYYMM VALUES LESS THAN (...), PARTITION audit_logs_max ...)`；只追加比现有最大边界更晚的月份，避免插入早于已有分区的边界。
- 分区内省：`information_schema.PARTITIONS`，返回名与 PG 的 `AuditPartitions` 对齐（兜底分区名不同，测试按引擎适配）。
- 自动清理：只删除名称符合 `audit_logs_YYYYMM` 且整月早于截止时间的分区；兜底分区永不删除。

### D7. MySQL 并发控制

| 场景 | PG | MySQL |
|---|---|---|
| 迁移 | `pg_advisory_xact_lock` | 独占连接上的 `GET_LOCK` |
| 分区维护 | 函数内 advisory lock | 独占连接上的 `GET_LOCK`（DDL 隐式提交） |
| 审计链追加 | advisory xact lock | `store_locks` 行 `SELECT ... FOR UPDATE` |
| 首个管理员引导 | advisory xact lock | `store_locks` 行 `SELECT ... FOR UPDATE` |

`store_locks(name PRIMARY KEY)` 由 MySQL 初始迁移创建；行锁随事务提交/回滚释放，适合跨进程串行化。

### D8. MySQL 写入策略

- 无 `RETURNING`：插入后 `LastInsertId`，需要完整行时在同一事务/连接内按主键回查。
- Upsert：`INSERT ... ON DUPLICATE KEY UPDATE <col> = VALUES(<col>), id = LAST_INSERT_ID(id)`，`LastInsertId` 对新增与已存在行都返回主键。
- `INSERT ... SELECT` 零行：先按角色 code 查询；缺失映射 `domain.ErrNotFound`，避免把“未插入”误判为成功。
- 错误映射：MySQL `1062` 重复键、`1451/1452` 外键冲突 → `domain.ErrConflict`；`sql.ErrNoRows` → `domain.ErrNotFound`。

### D9. MySQL 迁移

MySQL DDL 会隐式提交，无法沿用“单事务应用全部迁移”。每个文件在独占连接上通过 `GET_LOCK` 串行化执行；文件内语句写成幂等：

- `CREATE TABLE IF NOT EXISTS`、`DROP TRIGGER IF EXISTS` 后重建、`INSERT IGNORE` 种子。
- 记录写入 `schema_migrations(version, applied_at, checksum)`，校验和算法与 PG 相同。
- 部分失败后重跑即可继续；已应用文件内容变化时仍会因校验和不一致阻止启动。

### D10. 测试组织

`internal/store/storetest` 提供 `Run(t, Harness)`，覆盖用户、集群、凭据、绑定、设置、审计链、只追加、分区/保留、统计、迁移幂等。Harness 由各实现提供：

- 打开/重置数据库、执行原始 SQL、读取分区归属、临时绕过只追加（PG 禁用触发器；MySQL 删除后重建触发器）、期望迁移版本。
- 本地通过 `KUBEDECK_TEST_DATABASE_URL`（PG）与 `KUBEDECK_TEST_MYSQL_URL`（MySQL）分别启用；CI 两个 service 同时存在，不允许跳过。

### D11. 部署面

- Compose：保留 PG 文件为默认；新增独立完整文件 `docker-compose.mysql.yml`，避免 override 合并时基础文件的 `${POSTGRES_PASSWORD:?}` 仍被插值。`.env.mysql.example` 提供独立变量。
- Helm：新增 `database.driver`（默认 `postgres`），ConfigMap 输出 `KUBEDECK_DB_DRIVER`，NetworkPolicy 默认端口随驱动渲染；`secrets.databaseUrl` 与 `existingSecret` 契约不变。
- 离线包：新增 `MYSQL_IMAGE` 与 `--with-mysql` 开关，默认仍只带 PG；启用时把镜像加入导入/推送列表并拷贝 MySQL 清单与环境示例。

## Risks / Trade-offs

- **MySQL JSON 规范化差异** → 哈希只在写时由 Go 规范化，读回后重新解析再规范化；集成测试覆盖往返与篡改检测。
- **DDL 非事务导致迁移半完成** → 迁移语句幂等 + 校验和记录；文档说明重跑即可恢复。
- **`REORGANIZE PARTITION` 持有元数据锁** → 只在启动与每日维护补齐未来月份，通常操作空兜底分区；不在请求路径执行。
- **长期停机导致审计行落入兜底分区** → 兜底分区不参与保留删除；运维文档说明跨窗口停机后的影响与手工建分区方式。
- **两套仓储 SQL 重复** → 用同一一致性测试套件锁定行为；只允许存储包内出现引擎差异。
- **MySQL 权限要求更高**（建表、触发器、分区 DDL） → 部署文档给出最小授权与建库脚本。
- **跨引擎审计链不连续** → 规格明确不承诺；切换引擎需重建元数据。

## Migration Plan

1. 先落地接口与 PG 包迁移，跑通现有 PG 集成测试，确认零行为变化。
2. 再引入 MySQL 实现与双库测试；CI 增加 MySQL service。
3. 更新 Compose/Helm/离线包/文档；默认部署仍走 PG。
4. 回滚：代码回退到旧版本即可；MySQL 部署无历史数据包袱。PG 数据库无需变更（迁移文件未修改）。

## Open Questions

无。引擎定位、数据迁移边界与交付范围已在规划阶段确认。