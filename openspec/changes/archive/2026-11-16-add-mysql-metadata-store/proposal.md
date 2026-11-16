## Why

KubeDeck 的平台元数据层目前只能使用 PostgreSQL：驱动、SQL 方言、迁移脚本、审计分区与哈希链都绑定在 pgx 与 plpgsql 上，部署清单（Compose、Helm、离线包）也默认 PG。部分目标环境只提供 MySQL 8，无法选择 PostgreSQL，导致平台无法落地。同时，现有仓储实现与驱动强耦合，未来再接入其他引擎仍要重写业务代码。

## What Changes

- 元数据存储支持两种引擎：PostgreSQL 15+（默认，行为不变）与 MySQL 8.0.20+；通过 `KUBEDECK_DATABASE_URL` 与可选的 `KUBEDECK_DB_DRIVER` 选择，驱动与连接串不一致时启动失败。
- 存储层改为“一个仓储接口 + 每引擎一个实现”：业务层只依赖 `store.Store` 及既有窄接口，新增引擎只需实现接口、迁移与一致性测试。
- MySQL 实现保持与 PG 等价的行为：用户/集群/凭据/角色绑定/设置的读写语义、错误映射、审计只追加、哈希链校验、按月分区与保留期清理。
- 审计哈希链按引擎各自计算并在本引擎内校验；不承诺跨引擎哈希一致，也不提供 PG → MySQL 数据迁移（切换引擎视为新库重建）。
- 数据层集成测试在同一套用例上分别跑真实 PostgreSQL 与真实 MySQL；CI 同时提供两个数据库服务。
- Compose、Helm、离线交付包与运维文档增加 MySQL 路径，PG 仍为默认。

## Capabilities

### New Capabilities

- `metadata-store`: 平台元数据的存储引擎选择、仓储契约、迁移与启动语义、跨引擎行为一致性，以及审计持久化（只追加、哈希链、分区保留）在两种引擎上的保证。

### Modified Capabilities

- `engineering-baseline`: 数据层集成测试的 CI 门禁从“连接真实 PostgreSQL”扩展为“连接真实 PostgreSQL 与真实 MySQL 分别执行，不得因缺少数据库环境而跳过”。

## Impact

- 代码：`internal/store` 拆分为接口根包与 `postgres`/`mysql` 实现；新增共享一致性测试；`internal/app` 的数据库装配与 `internal/config` 的驱动配置。
- 依赖：新增 MySQL 驱动（`github.com/go-sql-driver/mysql`）；pgx 保留。
- 部署：`deploy/compose` 新增 MySQL 清单与环境示例；Helm 新增 `database.driver` 与对应 ConfigMap/NetworkPolicy 渲染；`scripts/offline-bundle.sh` 支持随包携带 MySQL 镜像。
- 测试与 CI：`Makefile` 增加 MySQL 数据层测试入口；`.github/workflows/ci.yml` 增加 MySQL service。
- 文档：README、部署/离线/本地环境/运维/备份恢复/发布检查清单与 AGENTS.md 明确双引擎支持及切换边界。
- 不涉及：K8s 集群接入方式、API 契约、前端行为、Redis 会话存储。