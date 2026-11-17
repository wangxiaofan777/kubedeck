## Why

当前仓库并行维护多条部署路径：Helm Chart（13 个模板）、Compose 三种变体（PostgreSQL / MySQL / 离线覆盖）、离线打包脚本、Prometheus 规则，以及 30 KB 以上的部署与离线文档。实际交付与验收只走 Docker Compose 单机部署，其余路径长期没有执行验证，理解、修改和排障成本远高于收益。

本次把部署收敛到一条路径：Dockerfile + 单文件 Compose（应用 + PostgreSQL）+ 一份部署文档。不改变应用运行时行为。

## What Changes

- 删除 `deploy/helm/`（Helm Chart）与 `deploy/prometheus/`（告警规则）。
- 删除 `scripts/offline-bundle.sh`、`scripts/check-prometheus-rules.sh`、`dist/offline/` 产物。
- Compose 收敛为单一 `deploy/compose/docker-compose.yml`：应用 + PostgreSQL；删除 MySQL 变体、离线覆盖文件、两个 Caddyfile 与 MySQL 环境变量示例。
- TLS 终止交给部署环境已有的反向代理/网关，仓库不再内置 Caddy。
- 文档合并为 `docs/deployment.md`；删除 `deploy/compose/README.md`、`docs/offline-deployment.md`、`docs/alerting.md`。
- `Makefile` 删除 `compose-*-mysql`、`offline-bundle`、`compose-up-offline`、`test-rules` 目标。
- 规格：工程基线新增“部署制品收敛”、改写“会话与部署副本一致性”；Kubernetes 兼容规格删除 Helm 渲染需求。

不在本次范围：

- 应用运行时行为、API、数据库 schema、MySQL 后端支持（仍可通过连接串使用外部 MySQL）。
- `/metrics` 端点与审计能力；只是不再随仓库分发 Prometheus 告警规则。
- 多副本与共享会话存储；当前仍固定单实例。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `engineering-baseline`：新增“部署制品收敛”；“会话与部署副本一致性”改为面向 Compose 单实例。
- `kubernetes-version-compat`：删除“兼容的部署清单”（Helm Chart 不再维护），目标集群 1.18–1.37 的兼容承诺不变。

## Impact

- **删除文件**：`deploy/helm/**`、`deploy/prometheus/**`、`deploy/compose/{README.md,docker-compose.mysql.yml,docker-compose.offline.yml,Caddyfile,Caddyfile.offline,.env.mysql.example}`、`scripts/{offline-bundle.sh,check-prometheus-rules.sh}`、`docs/{offline-deployment.md,alerting.md}`。
- **修改文件**：`deploy/compose/docker-compose.yml`、`deploy/compose/.env.example`、`docs/deployment.md`、`docs/operations.md`、`docs/release-checklist.md`、`docs/backup-restore.md`、`docs/README.md`、`README.md`、`AGENTS.md`、`Makefile`、`k8s-console-design.md`。
- **部署要求**：Docker Engine 24+ 与 Compose v2、PostgreSQL 15+、OIDC IdP、可终止 HTTPS 的反向代理。
- **迁移**：使用 Helm 的部署方改用 Compose 或自行维护清单；镜像与数据库 schema 不变。