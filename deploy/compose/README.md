# Docker Compose 单机部署

本目录提供 KubeDeck 的非 Kubernetes 部署方式：KubeDeck + PostgreSQL + Caddy TLS 反向代理。适合单机、小团队、演示和边缘环境。

> 应用固定 **单副本**。会话保存在进程内存，后台任务没有选主；需要多副本/高可用请使用 Helm Chart。

## 1. 前置条件

| 组件 | 要求 |
|---|---|
| Docker Engine | 24+，含 Compose v2 |
| 域名 | 一个解析到本机的域名；Caddy 用它申请证书 |
| OIDC IdP | 支持授权码 + PKCE，提供 `sub`、`email`、`groups` |
| 端口 | 80、443 可用 |
| 目标集群 | apiserver 从容器网络可达（见 §6） |

本地纯 HTTP 调试可把 `KUBEDECK_DOMAIN` 设为 `localhost`，Caddy 会使用本地 CA；生产必须使用真实域名和 HTTPS。

## 2. 启动

```bash
cd deploy/compose
cp .env.example .env
chmod 600 .env
```

编辑 `.env`，至少填写：

- `KUBEDECK_DOMAIN`：例如 `kubedeck.example.com`
- `POSTGRES_PASSWORD`：建议 `openssl rand -hex 24` 生成（避免 `@ : /` 等需要 URL 编码的字符）
- `KUBEDECK_MASTER_KEY`：`openssl rand -base64 32`
- OIDC 四项：issuer、client id、client secret、redirect URL
- `KUBEDECK_BOOTSTRAP_TOKEN`：至少 16 个字符，建议 `openssl rand -hex 24`

启动：

```bash
docker compose up -d --build
docker compose ps
```

也可以在仓库根目录执行 `make compose-up`。首次构建会拉取 Node、Go、distroless、PostgreSQL 和 Caddy 镜像，耗时取决于网络。

检查：

```bash
curl -fsS https://kubedeck.example.com/healthz
curl -fsS https://kubedeck.example.com/readyz
docker compose logs -f kubedeck
```

`/readyz` 会检查 PostgreSQL；数据库未就绪时返回 503。容器健康检查由镜像内的 `/healthcheck` 完成，不依赖 shell/curl。

## 3. 首次登录与引导管理员

1. 浏览器打开 `https://kubedeck.example.com`，完成 OIDC 登录。
2. 用 `KUBEDECK_BOOTSTRAP_TOKEN` 创建首个平台管理员：

```bash
curl -X POST https://kubedeck.example.com/auth/bootstrap \
  -H 'Content-Type: application/json' \
  -H "X-Bootstrap-Token: $KUBEDECK_BOOTSTRAP_TOKEN" \
  -d '{"subject":"<OIDC sub>","email":"admin@example.com","displayName":"平台管理员"}'
```

成功后接口返回 201，再次调用返回 409。之后在 `/api/v1/admin/users` 查看用户并配置角色绑定。

## 4. 接入集群

控制台目前没有“接入集群”表单，使用 API 登记。请求体支持两种凭据：

**kubeconfig**

```bash
curl -X POST https://kubedeck.example.com/api/v1/clusters \
  -H 'Content-Type: application/json' \
  -b 'kubedeck_session=<浏览器 Cookie>' \
  -d '{"name":"prod","displayName":"生产集群","kubeconfig":"<kubeconfig 文本>"}'
```

**server + token + CA**

```bash
curl -X POST https://kubedeck.example.com/api/v1/clusters \
  -H 'Content-Type: application/json' \
  -b 'kubedeck_session=<浏览器 Cookie>' \
  -d '{
    "name":"prod",
    "displayName":"生产集群",
    "server":"https://10.0.0.10:6443",
    "token":"<ServiceAccount Token>",
    "caData":"<PEM>"
  }'
```

凭据会用主密钥加密后写入 PostgreSQL。平台只接受 `https://` apiserver，拒绝 `169.254.0.0/16`，并拒绝 kubeconfig 中的 `exec`/`auth-provider`。

被管理集群需要一个只授予 `impersonate` 的 ServiceAccount/ClusterRole，参考 [`docs/permission-matrix.md`](../../docs/permission-matrix.md) 与 Helm chart 中的 `serviceaccount-rbac.yaml`。不要把 `cluster-admin` 交给平台。

## 5. 域名、TLS 与 Cookie

- DNS 的 A/AAAA 记录指向部署主机。
- 路由器/防火墙放行 80 和 443；Caddy 自动完成 ACME 验证与续期。
- `KUBEDECK_OIDC_REDIRECT_URL` 必须与 IdP 中登记的回调完全一致，通常是 `https://<域名>/auth/callback`。
- 生产保持 `KUBEDECK_COOKIE_INSECURE=false`。如果通过纯 HTTP 访问，Secure Cookie 不会发送，表现为登录后立即掉线；这不是权限问题。
- Caddy 只把 80/443 暴露到宿主机，应用 8080 不映射；`/metrics` 因此也不会直接暴露到公网。

## 6. 目标集群网络（WSL2 / Docker Desktop 重点）

容器里的 `127.0.0.1` 是容器自己，不是宿主机，也不是 WSL。若 k3s 跑在 WSL2、Docker 跑在 Windows Docker Desktop，容器默认无法通过 `127.0.0.1:6443` 访问 k3s。按环境选择：

1. **推荐：把 Compose 跑在 WSL2 内**（在 WSL2 安装 Docker Engine）。使用默认 bridge 网络，登记 `server: https://<WSL eth0 IP>:6443`。k3s 默认监听所有网卡，证书通常已包含节点 IP；可用下面的命令确认 SAN：
   ```bash
   openssl s_client -connect <WSL-IP>:6443 -servername <WSL-IP> </dev/null 2>/dev/null \
     | openssl x509 -noout -text | grep -A1 'Subject Alternative Name'
   ```
2. **Compose 跑在 Windows Docker Desktop**：容器在独立 Linux VM 中，`host.docker.internal` 指向 Windows 而不是 WSL。优先尝试 WSL 的 eth0 IP（要求 Windows 路由可达且证书 SAN 覆盖）；如果不可路由，把 k3s apiserver 暴露到一个容器可达的地址，再登记该地址。
3. **仅本地临时验证**：在上述可达地址基础上使用 `insecureSkipTLSVerify: true`。生产不要使用。

如果 kubeconfig 里的 server 是 `127.0.0.1:6443`，登记前把它替换成容器可达的地址；也可以改用 `server + token + caData` 形式，避免修改整份 kubeconfig。排查连通性时可以用一次性容器：

```bash
docker run --rm curlimages/curl -vk https://<可达地址>:6443/version
```

## 7. 单副本限制

- 不要执行 `docker compose up --scale kubedeck=2`。会话、WebSocket 连接和后台任务都在进程内，多副本会导致登录态漂移、重复健康探测/审计维护。
- 升级/重启会让所有用户重新登录。
- 需要零停机、滚动发布或多副本，请使用 Helm Chart；或先实现 Redis 会话与后台任务选主。

## 8. 备份与恢复

数据库备份：

```bash
docker compose exec -T db pg_dump -U kubedeck -Fc kubedeck > kubedeck-$(date +%F).dump
```

恢复（先停应用，避免写入）：

```bash
docker compose stop kubedeck
docker compose exec -T db dropdb -U kubedeck --if-exists kubedeck_restore
docker compose exec -T db createdb -U kubedeck kubedeck_restore
docker compose exec -T db pg_restore -U kubedeck -d kubedeck_restore < kubedeck-2026-11-13.dump
# 校验行数后，将恢复库改名为 kubedeck，或直接恢复到原库
docker compose start kubedeck
```

主密钥必须与数据库备份分开保管：只有数据库备份无法解密已登记的集群凭据；只有主密钥无法恢复元数据。轮换流程见 [`docs/backup-restore.md`](../../docs/backup-restore.md) 与 [`docs/deployment.md`](../../docs/deployment.md)。

## 9. 升级与回滚

```bash
# 生产建议使用不可变 tag
KUBEDECK_IMAGE=registry.example.com/kubedeck:0.2.0 docker compose up -d

# 本地源码构建
docker compose up -d --build
```

单副本升级会有短暂中断。回滚时把 `KUBEDECK_IMAGE` 切回上一个 tag 并 `docker compose up -d`；如果新版本执行了不兼容迁移，需要同时恢复数据库备份。

## 10. 监控

`/metrics` 不对外发布。需要抓取时，把 Prometheus 加入同一个 Compose 网络，目标写 `kubedeck:8080`。告警规则在 `deploy/prometheus/kubedeck-rules.yaml`，但它是 Prometheus Operator 的 `PrometheusRule`，非 Operator 环境需要把 `spec.groups` 抽出为普通规则文件：

```bash
python3 - <<'PY'
import yaml
doc = yaml.safe_load(open('deploy/prometheus/kubedeck-rules.yaml'))
yaml.safe_dump({'groups': doc['spec']['groups']}, open('kubedeck-rules.yml','w'), allow_unicode=True)
PY
```

## 11. 安全注意事项

- `.env` 权限保持 `0600`，不要提交仓库；`.gitignore` 已忽略 `deploy/compose/.env`。
- 环境变量会被 `docker inspect` 和拥有 Docker 权限的用户读取。生产更严格时应使用外部 Secret 管理系统；当前版本不支持 `_FILE` 形式的密钥注入。
- 主密钥只放在受控主机或 Vault/KMS，不要写进镜像、Helm values 或聊天记录。
- 不要把 8080、5432 映射到公网；不要在生产使用开发模式。
- `/metrics` 无鉴权，只允许内网抓取。

## 12. 故障排查

| 现象 | 排查 |
|---|---|
| `db` 一直 unhealthy | `docker compose logs db`；确认密码与卷中的旧数据一致；旧卷不会因改 `.env` 自动改密码 |
| 应用反复重启 | `docker compose logs kubedeck`；检查主密钥是否为 32 字节 base64、数据库 URL 是否正确、OIDC 是否可达 |
| `/readyz` 返回 503 | 数据库连接失败或迁移未完成；先看 `db` 健康状态 |
| 登录后立即掉线 | 是否通过 HTTPS 访问；`KUBEDECK_COOKIE_INSECURE=false` 时 Secure Cookie 不会在 HTTP 下发送 |
| OIDC 回调报错 | `KUBEDECK_OIDC_REDIRECT_URL` 与 IdP 登记值是否完全一致；域名是否与 issuer 匹配 |
| 502/504 | 应用未就绪；`docker compose ps` 看健康状态，`docker compose logs proxy kubedeck` |
| Caddy 证书失败 | DNS 是否解析到本机、80/443 是否放行；`localhost` 使用本地 CA，浏览器会提示不受信任 |
| 无法连接目标集群 | 用 §6 的临时容器测试；检查证书 SAN、WSL/Docker 网络、apiserver 是否只监听 127.0.0.1 |
| 80/443 被占用 | 修改 `docker-compose.yml` 的端口映射，并同步调整 `KUBEDECK_OIDC_REDIRECT_URL` |

## 13. 常用命令

```bash
docker compose ps
docker compose logs -f kubedeck
docker compose restart kubedeck
docker compose down            # 保留命名卷
docker compose down -v         # 删除数据库与 Caddy 数据，谨慎
docker compose config          # 渲染并检查配置
```