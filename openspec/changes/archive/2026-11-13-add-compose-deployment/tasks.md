## 1. 健康检查工具与镜像

- [x] 1.1 新增 `cmd/healthcheck/main.go`：向 `http://127.0.0.1:8080/healthz` 发起 GET，2 秒超时，非 200 或连接失败返回非零；支持通过 `KUBEDECK_HEALTHCHECK_URL` 覆盖地址。验证：`go test ./cmd/healthcheck/` 覆盖成功、非 200、超时/连接失败三类分支。
- [x] 1.2 更新 `Dockerfile`：在构建阶段编译 healthcheck、复制到运行镜像，并添加 `HEALTHCHECK` 指令。验证：`docker build`（或 `make server`）成功，`docker inspect` 能看到健康检查配置或镜像内存在 `/healthcheck`。

## 2. Compose 编排

- [x] 2.1 新增 `deploy/compose/docker-compose.yml`：`kubedeck`、`db`、`proxy` 三个服务；应用 `restart: unless-stopped`、`read_only`、`tmpfs`、`cap_drop: ALL`、`no-new-privileges`，通过 `env_file` 读取配置，依赖 `db` 健康；PostgreSQL 使用命名卷且不映射宿主机端口；只有 `proxy` 发布 80/443。验证：`docker compose -f deploy/compose/docker-compose.yml config` 无错误，且渲染结果中应用无宿主机端口。
- [x] 2.2 新增 `deploy/compose/Caddyfile`：按域名反代到 `kubedeck:8080`，支持 WebSocket，生产自动 HTTPS；域名通过环境变量注入。验证：`caddy validate --config deploy/compose/Caddyfile`（或 `docker run caddy caddy validate`）通过。
- [x] 2.3 新增 `deploy/compose/.env.example`：覆盖数据库密码/连接串、主密钥与版本、OIDC 四项、引导令牌、域名、Cookie 安全开关；全部为占位值，不含真实密钥。验证：对照 `internal/config/config.go` 与 Helm ConfigMap，确认生产必需项齐全；`git grep` 确认无真实凭据。
- [x] 2.4 在 `Makefile` 增加 `compose-up`、`compose-down`、`compose-config` 便捷目标，仅封装 `docker compose -f deploy/compose/docker-compose.yml`。验证：`make compose-config` 输出渲染后的配置。

## 3. 文档

- [x] 3.1 新增 `deploy/compose/README.md`：启动步骤、首次 bootstrap、集群接入、WSL2/Docker Desktop 网络三种方案、域名与 TLS、单副本限制、备份与升级、故障排查。验证：按文档从空目录执行命令可复现，命令与 Compose 文件一致。
- [x] 3.2 更新根 `README.md`、`docs/README.md`、`docs/deployment.md`：增加 Compose 部署入口与“非 Kubernetes 部署”章节，明确适用/不适用场景。验证：链接可达，Helm 仍是生产 HA 的推荐路径。
- [x] 3.3 在文档中明确密钥与主密钥保管、`/metrics` 不对外、环境变量可被 `docker inspect` 读取等安全注意事项。验证：与 `docs/security-review.md`、`docs/backup-restore.md` 的结论一致，无矛盾。

## 4. 验收与归档

- [x] 4.1 本地构建镜像并启动 Compose 栈，验证 `db` 健康、应用 `/healthz` 与 `/readyz` 返回 200、登录页可访问。验证：`docker compose ps` 与 `curl` 结果。
- [x] 4.2 验证数据持久化与安全基线：重启 `db` 后平台元数据仍在；应用容器以 nonroot 运行、根文件系统只读；应用端口未直接发布到宿主机。验证：`docker compose restart db`、`docker inspect`、`docker compose ps`。
- [x] 4.3 验证至少一种目标集群接入路径（本地 k3s 或远程集群）：登记集群、浏览 Pod、打开日志。验证：控制台与 `/api/v1/clusters` 返回正常。
- [x] 4.4 执行 `make check`、`openspec validate add-compose-deployment --strict`，确认通过后提交并归档。验证：命令退出码为 0，`git status` 干净。