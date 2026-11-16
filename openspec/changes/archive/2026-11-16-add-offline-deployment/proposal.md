## Why

现有部署文档默认目标机器可以访问 Docker Hub、Go 模块代理和 npm registry。内网、等保或边缘环境往往只有一台能上网的构建机，目标机器完全隔离；当前仓库没有镜像导出/导入流程，也没有说明离线时 TLS 证书与 OIDC IdP 如何解决，落地时容易卡在拉取镜像、ACME 签发和 OIDC discovery 上。

本变更补齐离线交付路径：在联网机构建并导出镜像，目标机导入后分别按 Compose 或 Helm 部署，同时把内网 TLS 与 OIDC 的替代方案写清楚。不改变应用运行时行为。

## What Changes

- 新增 `scripts/offline-bundle.sh`：
  - 用 `docker buildx` 构建指定平台（默认 `linux/amd64`）的 KubeDeck 镜像；
  - 导出 KubeDeck 镜像，以及 Compose 需要的 `postgres:17-alpine`、`caddy:2-alpine`，统一 gzip 压缩；
  - 生成 `SHA256SUMS`、`manifest.txt`（版本、平台、镜像清单、Git commit、构建时间）和可直接拷走的 Compose/Helm 交付物。
- 新增 `docs/offline-deployment.md`：镜像体积、导出/传输/导入（Docker、containerd、内网 registry）、Compose 与 Helm 两种离线部署、内网 TLS、内网 OIDC、升级回滚与验收清单。
- 新增 `deploy/compose/docker-compose.offline.yml` 与 `deploy/compose/Caddyfile.offline`：
  - 离线覆盖文件禁止拉取镜像（`pull_policy: never`）；
  - Caddy 改用内部 CA（`tls internal`）或挂载自有证书，避免离线环境 ACME 必然失败。
- `Makefile` 增加 `offline-bundle` 与 `compose-up-offline` 便捷目标。
- Helm Chart 增加 `extraVolumes` / `extraVolumeMounts`，用于挂载内网 CA 并配合 `SSL_CERT_FILE` 信任离线 IdP 的私有证书。
- 根 `README.md`、`docs/README.md`、`docs/deployment.md`、`deploy/compose/README.md` 增加离线部署入口。

不在本次范围：

- 内置 OIDC IdP 或本地账号体系；离线环境仍需自备 Keycloak/Dex 等 IdP。
- 修改镜像内容、应用配置项、API 契约或数据库 schema。
- 内置镜像仓库；内网 registry 只提供导入与推送命令示例。
- 多架构单包（一次打包同时包含 amd64/arm64）；脚本一次产出一个平台，需要时跑两次。

## Capabilities

### New Capabilities

无。本次只新增交付脚本、部署编排和文档，不改变系统对外行为契约。

### Modified Capabilities

无。认证、集群接入、资源浏览、工作负载、日志、实时、审计、控制台等能力规格均不变；`.openspec.yaml` 已声明 `skip_specs: true`。

## Impact

- **新增文件**：`scripts/offline-bundle.sh`、`docs/offline-deployment.md`、`deploy/compose/docker-compose.offline.yml`、`deploy/compose/Caddyfile.offline`。
- **修改文件**：`Makefile`、`README.md`、`docs/README.md`、`docs/deployment.md`、`deploy/compose/README.md`、`deploy/helm/kubedeck/values.yaml`、`deploy/helm/kubedeck/templates/deployment.yaml`。
- **运行时依赖**：不变；离线部署仍需目标集群可达、PostgreSQL 15+、OIDC IdP 可达。
- **交付物**：脚本产出的镜像包与校验文件不进入版本库（输出到 `dist/offline/`，由 `.gitignore` 忽略）。