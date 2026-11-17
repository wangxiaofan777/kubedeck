## 1. 规格与提案

- [x] 1.1 创建 `openspec/changes/simplify-deployment-to-compose`（proposal/design/tasks）
- [x] 1.2 新增工程基线 delta：部署制品收敛、会话与部署副本一致性
- [x] 1.3 新增 Kubernetes 兼容 delta：删除“兼容的部署清单”
- [x] 1.4 `openspec validate simplify-deployment-to-compose --strict` 通过

## 2. 删除并行部署路径

- [x] 2.1 删除 `deploy/helm/` 与 `deploy/prometheus/`
- [x] 2.2 删除 Compose 变体：`docker-compose.mysql.yml`、`docker-compose.offline.yml`、`Caddyfile`、`Caddyfile.offline`、`.env.mysql.example`、`README.md`
- [x] 2.3 删除 `scripts/offline-bundle.sh`、`scripts/check-prometheus-rules.sh`
- [x] 2.4 删除 `docs/offline-deployment.md`、`docs/alerting.md` 与 `dist/offline/`
- [x] 2.5 保留未跟踪的 `deploy/compose/.env` 本地配置，不提交、不删除

## 3. 收敛部署制品

- [x] 3.1 `docker-compose.yml` 只保留应用 + PostgreSQL，移除 Caddy 服务与卷
- [x] 3.2 `.env.example` 移除域名/离线/MySQL 变体说明，补充前置 HTTPS 代理要求
- [x] 3.3 重写 `docs/deployment.md`：前置条件、启动、OIDC、管理员、接入集群、MySQL 连接、备份、升级回滚、网络与排障
- [x] 3.4 更新 `Makefile`：删除 mysql、offline、test-rules 目标与相关变量

## 4. 同步引用

- [x] 4.1 更新 `README.md` 部署章节与目录树
- [x] 4.2 更新 `AGENTS.md` 与 `docs/README.md` 的部署描述与文档索引
- [x] 4.3 更新 `docs/operations.md`、`docs/release-checklist.md`、`docs/backup-restore.md` 中的 Helm 命令
- [x] 4.4 更新 `k8s-console-design.md` 中“平台自身用 Helm 部署”的描述

## 5. 验收

- [x] 5.1 `rg` 确认非历史文档不再引用 Helm Chart、离线打包、Caddy 或 Prometheus 规则（`docs/acceptance.md`、`docs/release-notes.md`、`docs/security-review.md` 为历史记录，保留原文）
- [x] 5.2 `docker compose -f deploy/compose/docker-compose.yml config` 渲染通过；服务仅 `kubedeck` 与 `db`，端口绑定 `127.0.0.1:8080`
- [x] 5.3 本次未改动 Go 与前端代码，不执行 `make check`（当前 shell 无 Go/make）；部署变更以 Compose 渲染结果验收