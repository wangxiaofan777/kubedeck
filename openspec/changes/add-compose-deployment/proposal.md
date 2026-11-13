## Why

当前生产部署只提供 Helm Chart，必须有一个可用的 Kubernetes 集群才能安装 KubeDeck。对于单机、小团队、演示和边缘环境，用户更希望用一个 `docker compose up -d` 完成部署，而不必先维护一套 K8s。增加 Compose 部署方式可以显著降低落地门槛，同时保持应用镜像、配置项和安全约束不变。

## What Changes

- 新增 `deploy/compose/` 单机部署目录，包含：
  - `docker-compose.yml`：KubeDeck 应用、PostgreSQL 15+、Caddy TLS 反向代理三个核心服务；应用固定单副本。
  - `Caddyfile`：终止 TLS，反代到 `kubedeck:8080`，不暴露应用端口到公网。
  - `.env.example`：数据库、主密钥、OIDC、引导令牌等配置占位，明确标注哪些值绝不能提交。
  - `README.md`：启动步骤、集群接入、网络要求、备份、升级、单副本限制与故障排查。
- 为运行镜像增加健康检查能力：新增极小的 `cmd/healthcheck` 工具，在 Dockerfile 中构建并以 `HEALTHCHECK` 调用 `/healthz`，解决 distroless 镜像没有 shell/curl 的问题。该工具只做本机 HTTP 探活，不改变应用 API 行为。
- 在根 `README.md` 与 `docs/README.md` 增加 Compose 部署入口；在 `docs/deployment.md` 增加“非 Kubernetes 部署”指引，避免用户误以为只能用 Helm。
- 可选地增加 `make compose-up` / `make compose-down` 便捷目标（仅封装 `docker compose`，不改变构建流程）。

不在本次范围：

- 多副本/高可用、Redis 会话存储、后台任务选主。
- 在 Compose 中运行 k3s 或被管理集群；被管理集群仍通过 kubeconfig 或 `server + token + CA` 登记。
- 修改现有 OIDC、Impersonation、审计、实时推送等行为。
- 支持 Docker Compose 文件型 Secret（应用当前只读环境变量）；继续使用权限受限的 `.env` 文件，并在文档中说明风险。

## Capabilities

### New Capabilities

无。本次只新增部署编排与镜像探活工具，不改变系统对外的行为契约。

### Modified Capabilities

无。现有能力规格（认证、集群接入、资源浏览、工作负载、日志、实时、审计、控制台、工程基线）均不变；`.openspec.yaml` 已声明 `skip_specs: true`。

## Impact

- **新增文件**：`deploy/compose/{docker-compose.yml,Caddyfile,.env.example,README.md}`、`cmd/healthcheck/main.go`。
- **修改文件**：`Dockerfile`（构建并复制 healthcheck、增加 `HEALTHCHECK`）、根 `README.md`、`docs/README.md`、`docs/deployment.md`、`Makefile`（可选便捷目标）。
- **运行时依赖**：Docker Engine 24+ 与 Compose v2；PostgreSQL 15+；一个可达的 OIDC IdP；被管理集群的 apiserver 网络可达。
- **不涉及**：API 契约、数据库 schema、迁移、权限模型、审计格式、镜像入口参数。
- **部署边界**：Compose 方案固定单应用副本；多副本仍使用 Helm/K8s，或后续单独实现 Redis 会话与选主。