## Context

KubeDeck 目前只有 Helm 部署路径。应用本身是一个 Go 单二进制，前端通过 `go:embed` 打入，运行时镜像为 distroless nonroot；生产模式只依赖 PostgreSQL、OIDC IdP 和目标集群 apiserver。会话保存在进程内存，后台任务（健康探测、审计维护、会话清理）没有选主，因此当前架构只适合单副本。上述约束决定了 Compose 方案应保持“单应用副本 + 外部依赖”的形态，而不是把 Helm 的所有能力搬过来。动机见 `proposal.md`。

## Goals / Non-Goals

**Goals:**

- 提供一条命令可启动的单机部署：KubeDeck + PostgreSQL + TLS 反向代理。
- 默认安全：应用端口不直接暴露公网、非 root、只读根文件系统、Cookie Secure、主密钥不入库不入镜像。
- 复用现有镜像与配置项，不新增 API、数据库迁移或权限模型。
- 让镜像具备可用的容器健康检查，补齐 distroless 无法执行 shell 探针的短板。
- 文档覆盖集群接入、WSL/Docker 网络、备份、升级、单副本限制和故障排查。

**Non-Goals:**

- 多副本、高可用、滚动发布、Redis 会话、后台任务选主。
- 在 Compose 内运行 k3s 或被管理集群。
- 内置 Prometheus/Grafana/Alertmanager 全套监控；只保证 `/metrics` 可被抓取，文档给出接入方式。
- 支持 Docker Compose 文件型 Secret（应用只读环境变量），本次不引入 `_FILE` 配置。
- 替代 Helm Chart 或 Kubernetes 部署文档。

## Decisions

### D1：三个核心服务，应用固定单副本

`docker-compose.yml` 只包含 `kubedeck`、`db`、`proxy` 三个服务。应用不设置 `deploy.replicas`，也不支持横向扩容。原因是会话在进程内存、后台任务无选主；多副本会导致登录态漂移和重复探测/维护。需要 HA 时继续使用 Helm，或后续单独实现共享会话与选主。

备选：把 Redis 也放进 Compose。当前代码没有使用 Redis，放进去只会增加一个无人使用的依赖，因此不采纳。

### D2：Caddy 作为 TLS 入口

选择 Caddy 2 而不是 nginx/Traefik：

- 自动申请和续期 Let's Encrypt 证书，Caddyfile 只有几行。
- 原生支持 WebSocket 反代，不需要额外 upgrade 配置。
- 适合单机部署，避免引入动态配置或 CRD。

备选：nginx 需要手写证书续期与 WS 头；Traefik 对单机场景偏重。Caddy 的代价是镜像略大，可接受。

生产流量只经过 `proxy` 的 80/443；`kubedeck:8080` 仅在 Compose 内部网络可达，不映射到宿主机。`/metrics` 同样不对外暴露，需要监控时由同网络的 Prometheus 抓取，或显式增加只监听内网的端口映射。

### D3：镜像内增加独立健康检查工具

distroless 镜像没有 shell、curl、wget，Compose 的 `healthcheck` 无法直接探 `/healthz`。新增 `cmd/healthcheck`：一个无第三方依赖的小程序，向 `http://127.0.0.1:8080/healthz` 发起 GET，2 秒超时，非 200 或连接失败返回非零。Dockerfile 在多阶段构建中编译它并复制到运行镜像，设置：

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD ["/healthcheck"]
```

备选：给主程序加 `-healthcheck` 参数。会把运维关注点混进业务入口，且需要改参数解析；独立工具更干净，也不改变应用行为。

### D4：配置通过 `.env` 注入，不引入文件型 Secret

应用当前只读环境变量，Compose 的 `secrets:` 挂载文件后无法被直接读取。本次使用 `env_file: .env`，要求文件权限 `0600`、不提交仓库、部署在受控主机。文档明确说明 `docker inspect` 能看到环境变量，生产更严格时应使用外部 Secret 管理系统或后续增加 `_FILE` 支持。

备选：在 Compose 中用一个 entrypoint 脚本把文件转成环境变量。运行镜像是 distroless，没有 shell，无法实现；为此换基础镜像会扩大攻击面，不采纳。

### D5：数据库与启动顺序

使用 `postgres:17-alpine`，命名卷 `pgdata` 持久化。数据库不映射宿主机端口。`db` 配置 `pg_isready` 健康检查，应用通过 `depends_on: {db: {condition: service_healthy}}` 延后启动；应用自身设置 `restart: unless-stopped`，数据库短暂不可用时由重启策略兜底。迁移仍由应用启动时执行，不增加独立 migration 服务。

### D6：容器安全基线

- 应用使用镜像自带的 `nonroot` 用户，`read_only: true`，`tmpfs: /tmp`，`cap_drop: [ALL]`，`security_opt: [no-new-privileges:true]`。
- 数据库与 Caddy 使用官方镜像默认用户；Caddy 仅授予绑定 80/443 所需能力。
- 所有服务加入内部网络；只有 `proxy` 发布端口。
- `.env.example` 使用明显占位符，不包含任何真实密钥。

### D7：集群网络与接入方式

Compose 只负责运行平台，被管理集群仍通过控制台/API 登记 kubeconfig 或 `server + token + CA`。应用容器必须能访问目标 apiserver 的 6443/443。对于 Docker Desktop + WSL2 k3s 的场景，容器内的 `127.0.0.1` 不是 WSL；文档给出三种方式：

1. 在 WSL2 内运行 Compose 并使用 `network_mode: host`；
2. 让 k3s 监听容器可达的地址，并确保证书 SAN 覆盖该地址；
3. 通过 `host.docker.internal` 或宿主机地址访问，并处理证书 SAN。

不接受为图省事默认开启 `insecureSkipTLSVerify`。

### D8：版本与升级

Compose 文件同时支持 `image:` 与 `build:`：生产固定镜像 tag，本地可用 `docker compose build`。升级流程为拉取新镜像/重新构建后 `docker compose up -d`；单副本会有短暂中断，用户会话会丢失（内存会话），需在文档中明确。回滚通过切回上一个镜像 tag 并恢复数据库备份完成。

## Risks / Trade-offs

- **单副本是单点故障** → `restart: unless-stopped`、宿主机级监控、定期 `pg_dump` 与恢复演练；需要 HA 时改用 Helm。
- **环境变量中的密钥可被 `docker inspect` 读取** → `.env` 权限 `0600`、专用主机、最小化登录用户；后续可增加 `_FILE` 支持。
- **主密钥丢失导致集群凭据不可恢复** → 文档要求与数据库备份分开保管，纳入备份检查单。
- **WSL2/Docker Desktop 网络不通** → 文档给出 host 网络、监听地址、证书 SAN 三种方案，并在验收时至少验证一种。
- **Caddy 自动 HTTPS 依赖域名与 80/443 可达** → 支持通过环境变量切换到自定义证书或本地 HTTP 调试；生产检查单要求域名解析与端口放行。
- **健康检查工具增加少量镜像体积与维护面** → 逻辑限制在单文件、无依赖、只读探活，并由 `go test` 覆盖 URL 与超时分支。
- **Compose 不做滚动更新** → 明确单机定位；对可用性敏感的部署继续使用 Kubernetes。

## Migration Plan

1. 在目标主机准备 `.env`（数据库密码、主密钥、OIDC 配置、域名），权限设为 `0600`。
2. `docker compose up -d --build`，等待 `db` 健康、应用 `/readyz` 通过。
3. 完成 OIDC 登录与 bootstrap，登记至少一个集群并验证列表/日志。
4. 配置宿主机级备份：每日 `pg_dump`，主密钥单独保管。
5. 升级：拉取新镜像 → `docker compose up -d` → 验证 `/readyz` 与登录；回滚则切回旧 tag 并按需恢复数据库。
6. 该方案是新增部署路径，不迁移既有 Helm 安装的数据；两者可以指向同一个 PostgreSQL，但同一时间只应运行一套应用实例（会话与后台任务不共享）。