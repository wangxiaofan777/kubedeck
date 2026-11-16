## 1. 存储抽象与 PostgreSQL 迁移

- [x] 1.1 在 `internal/store` 定义 `Store` 接口、共享 DTO（`AuditFilter`/`ChainStatus`/`AuditStats*`）、驱动解析与分页/条目规范化工具；`go build ./...` 通过
- [x] 1.2 将现有 PG 仓储、迁移与测试迁入 `internal/store/postgres`，SQL 与迁移文件不变；`go test ./internal/store/postgres/...` 在 PG 测试库上通过
- [x] 1.3 调整 `internal/app/app.go`：`openDatabase` 返回接口、按驱动选择实现、迁移/分区调用改为接口方法；`go build ./cmd/server` 通过且启动日志包含驱动名
- [x] 1.4 调整 `internal/config/config.go`：新增 `DatabaseDriver`/`KUBEDECK_DB_DRIVER` 与校验；为合法/冲突/不支持驱动补单元测试并全部通过

## 2. MySQL 连接与迁移

- [x] 2.1 引入 `github.com/go-sql-driver/mysql`，实现 `mysql.Open`：URL/native DSN、UTC/`parseTime`/`multiStatements`、连接池与 Ping；`go mod tidy` 后构建通过
- [x] 2.2 实现 MySQL 错误映射（1062/1451/1452、`sql.ErrNoRows`）并补单元测试
- [x] 2.3 编写 `internal/store/mysql/migrations/0001_init.sql`：全部表、索引、角色/设置种子、`store_locks`、审计只追加触发器、RANGE COLUMNS 兜底分区；在真实 MySQL 8 上首次启动建库成功
- [x] 2.4 实现 MySQL 迁移执行器：`GET_LOCK` 串行化、校验和、幂等重跑；连续两次启动迁移不重复且不报错

## 3. MySQL 仓储

- [x] 3.1 用户与平台管理员：Upsert、查询、列表、状态/管理员标记、引导；集成用例覆盖幂等 upsert 与唯一管理员
- [x] 3.2 集群与凭据：创建、原子创建集群+凭据、状态/连接更新、重命名、删除、凭据轮换与按主键版本查询；冲突与缺失映射用例通过
- [x] 3.3 角色绑定与设置：创建/幂等刷新、按用户/集群查询、整体覆盖、删除、设置读写；重复四元组与非法角色用例通过
- [x] 3.4 审计写入：`store_locks` 串行化、Go 哈希链、单条/批量追加、事务回滚；追加后链校验通过
- [x] 3.5 审计查询与统计：过滤、分页排序、按天/动词/资源/主体聚合；与 PG 相同夹具的断言通过

## 4. MySQL 审计分区与只追加

- [x] 4.1 实现 `AuditPartitions`/`EnsureAuditPartitions`/`DropExpiredAuditPartitions`：`information_schema.PARTITIONS`、REORGANIZE/DROP、保留期不删当前月与兜底分区；分区路由与保留期用例通过
- [x] 4.2 验证只追加触发器拒绝 UPDATE/DELETE，并验证测试可临时绕过触发器后篡改被链校验定位
- [x] 4.3 验证 MySQL 审计链在重启与批量写入后仍完整，`VerifyAuditChain` 对正常链与篡改链给出正确结果

## 5. 双引擎测试与 CI

- [x] 5.1 抽取 `internal/store/storetest` 一致性套件，PG/MySQL 测试入口分别读取 `KUBEDECK_TEST_DATABASE_URL` 与 `KUBEDECK_TEST_MYSQL_URL`；单引擎缺失时跳过并说明
- [x] 5.2 `Makefile` 增加 `test-db-mysql`/`db-mysql` 与 `scripts/dev-db-mysql.sh`；本地启动 MySQL 后 `make test-db-mysql` 全部通过
- [x] 5.3 CI backend job 增加 `mysql:8.0` service 与测试 URL，保持 PostgreSQL service；工作流语法检查通过
- [x] 5.4 `make lint && make test` 通过；PG 与 MySQL 两套集成测试均通过

## 6. 部署与交付

- [x] 6.1 新增 `deploy/compose/docker-compose.mysql.yml` 与 `.env.mysql.example`；`docker compose ... config` 渲染通过
- [x] 6.2 Helm：`values.yaml` 增加 `database.driver`，ConfigMap 输出驱动，NetworkPolicy 按驱动渲染 3306/5432；`helm template --set database.driver=mysql` 输出正确
- [x] 6.3 离线包脚本支持 `MYSQL_IMAGE` 与 `--with-mysql`，默认仍只带 PG；脚本在 `--no-deps` 与含依赖两种模式下的 dry-run/清单检查通过
- [x] 6.4 `Makefile` 增加 MySQL Compose 目标并在 README 中记录入口

## 7. 文档与收尾

- [x] 7.1 更新 README、docs/README、deployment、offline-deployment、local-environment、operations、backup-restore、release-checklist、compose README：MySQL 建库授权、DSN、备份、切换引擎边界
- [x] 7.2 更新 `AGENTS.md` 技术栈与约束措辞为引擎无关，保留"集群资源状态不入库"
- [x] 7.3 执行 `openspec validate add-mysql-metadata-store --strict`、`make check` 与双库集成测试，记录结果并勾选任务