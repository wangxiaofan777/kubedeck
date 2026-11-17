# 部署手册

KubeDeck 以单个容器镜像交付，使用 Docker Compose 单机部署：一个应用实例 + 一个 PostgreSQL。前端资源已嵌入后端二进制，镜像内没有 Nginx、Node 或独立前端服务。

> 本地开发与验收环境见 [local-environment.md](./local-environment.md)。不要把本文中的占位符替换成真实凭据后提交到仓库。

## 1. 部署模型与边界

- **只支持单实例**：登录会话、OIDC 访问令牌、WebSocket 连接与后台任务都在进程内。不要执行 `docker compose up --scale kubedeck=2`；多实例会导致登录态漂移和重复健康探测。零停机升级与水平扩容需要先引入共享会话存储，当前不支持。
- **HTTPS 由前置代理终止**：Compose 默认只把应用绑定到 `127.0.0.1:8080`，由同机反向代理对外提供 HTTPS。生产保持 `KUBEDECK_COOKIE_INSECURE=false`，纯 HTTP 访问会表现为登录后立即掉线。
- **目标集群独立**：KubeDeck 自身不需要运行在 Kubernetes 中；被管理的集群通过 kubeconfig 或 `server + token + CA` 登记，平台到 apiserver 必须网络可达。
- **数据库**：默认使用 Compose 内的 PostgreSQL 17。也可以指向外部 PostgreSQL 15+ 或 MySQL 8.0.20+（见 §7.3）。

## 2. 前置条件

| 组件 | 要求 |
|---|---|
| Docker Engine | 24+，含 Compose v2 |
| 数据库 | PostgreSQL 15+（默认）或 MySQL 8.0.20+ |
| OIDC IdP | 支持授权码 + PKCE，提供 `sub`、`email`、`groups`；`email` 存在时默认要求 `email_verified=true` |
| 入口 | 一个解析到部署主机的域名，以及能终止 HTTPS 的反向代理 |
| 目标集群 | Kubernetes 1.18–1.37；低于 1.18 拒绝接入 |
| 主机 | 可访问目标集群 apiserver 与 OIDC IdP；不对公网开放 5432/3306/8080 |

## 3. 准备配置与启动

```bash
cd deploy/compose
cp .env.example .env
chmod 600 .env
```

至少填写：

- `POSTGRES_PASSWORD`：`openssl rand -hex 24` 生成（避免 `@ : /` 等需要 URL 编码的字符）。
- `KUBEDECK_MASTER_KEY`：`openssl rand -base64 32`，32 字节 base64。丢失后已登记的集群凭据无法解密，必须与数据库备份分开保管。
- OIDC 四项：issuer、client id、client secret、redirect URL。
- `KUBEDECK_BOOTSTRAP_TOKEN`：至少 16 个字符，建议 `openssl rand -hex 24`。

启动：

```bash
docker compose up -d --build
docker compose ps
```

也可以在仓库根目录执行 `make compose-up`。首次构建会拉取 Node、Go、distroless 与 PostgreSQL 镜像。

检查：

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
docker compose logs -f kubedeck
```

`/readyz` 会检查数据库，数据库未就绪时返回 503。容器健康检查由镜像内的 `/healthcheck` 完成，不依赖 shell 或 curl。

## 4. 初始化管理员

1. 通过 HTTPS 入口打开控制台，完成一次 OIDC 登录。
2. 用引导令牌创建首个平台管理员：

```bash
curl -X POST https://kubedeck.example.com/auth/bootstrap \
  -H 'Content-Type: application/json' \
  -H "X-Bootstrap-Token: $KUBEDECK_BOOTSTRAP_TOKEN" \
  -d '{"subject":"<OIDC sub>","email":"admin@example.com","displayName":"平台管理员"}'
```

成功后返回 201，再次调用返回 409。之后在 `/api/v1/admin/users` 查看用户并配置角色绑定。建议创建完成后从 `.env` 删除引导令牌并重启应用。

## 5. 接入目标集群

在**每个被管理集群**创建只允许 impersonate 的平台 ServiceAccount。禁止绑定 `cluster-admin`：

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kubedeck-platform
  namespace: kubedeck-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubedeck-impersonator
rules:
  - apiGroups: [""]
    resources: ["users", "groups", "serviceaccounts"]
    verbs: ["impersonate"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kubedeck-impersonator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kubedeck-impersonator
subjects:
  - kind: ServiceAccount
    name: kubedeck-platform
    namespace: kubedeck-system
```

生成 kubeconfig（生产建议使用长期 ServiceAccount token Secret 或证书）：

```bash
TOKEN=$(kubectl -n kubedeck-system create token kubedeck-platform --duration=24h)
CA=$(kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
cat > platform-sa.kubeconfig <<EOF
apiVersion: v1
kind: Config
clusters:
- name: target
  cluster:
    server: https://target.example.com:6443
    certificate-authority-data: $CA
users:
- name: kubedeck-platform
  user:
    token: $TOKEN
contexts:
- name: target
  context: {cluster: target, user: kubedeck-platform}
current-context: target
EOF
```

验证：

```bash
kubectl --kubeconfig platform-sa.kubeconfig auth can-i impersonate users      # yes
kubectl --kubeconfig platform-sa.kubeconfig auth can-i list pods -A          # no
```

登录控制台后取得会话 Cookie（浏览器开发者工具的 `kubedeck_session`），通过 API 登记：

```bash
jq -n --rawfile kubeconfig platform-sa.kubeconfig \
  '{name:"prod-a",displayName:"生产 A",labels:{env:"prod"},kubeconfig:$kubeconfig}' \
  >/tmp/register.json

curl -X POST https://kubedeck.example.com/api/v1/clusters \
  -b 'kubedeck_session=<cookie>' \
  -H 'Content-Type: application/json' \
  --data-binary @/tmp/register.json

rm -f /tmp/register.json platform-sa.kubeconfig
```

也支持 `server + token + caData` 形式。登记时会探测 `/version` 与 discovery，低于最低支持版本（默认 1.18）或无法连通会返回明确错误且不落库；凭据以 AES-256-GCM 加密存储，接口只返回脱敏元数据。

## 6. 前置 HTTPS 反向代理

反向代理只需把 `https://<域名>` 转发到 `127.0.0.1:8080`，并满足两点：

- **WebSocket 与日志流不能缓冲**，否则实时推送会延迟。
- 生产使用受信任证书；浏览器到代理必须是 HTTPS。

Caddy 示例（同机运行，自动申请证书）：

```caddyfile
kubedeck.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
	}
}
```

Nginx 需要 `proxy_set_header Upgrade $http_upgrade;`、`proxy_set_header Connection "upgrade";` 和 `proxy_buffering off;`。

`KUBEDECK_OIDC_REDIRECT_URL` 必须与 IdP 中登记的回调完全一致，通常是 `https://<域名>/auth/callback`。`/metrics` 不对外暴露；需要抓取时让监控系统直接访问应用容器或内网地址。

## 7. 数据库

### 7.1 备份

```bash
docker compose exec -T db pg_dump -U kubedeck -Fc kubedeck > kubedeck-$(date +%F).dump
```

恢复前先停止应用，避免写入。主密钥与数据库备份必须分开保管：只有备份无法解密集群凭据，只有主密钥无法恢复元数据。完整流程见 [backup-restore.md](./backup-restore.md)。

### 7.2 默认 PostgreSQL

Compose 内的 PostgreSQL 数据保存在命名卷 `kubedeck_pgdata`。`docker compose down` 不会删除数据，`down -v` 会删除，谨慎使用。

### 7.3 切换到外部 MySQL 8

后端同时支持 MySQL 8.0.20+，但 Compose 只为默认路径提供数据库服务。使用外部 MySQL 时：

1. 准备库与账号，并开启审计触发器所需开关：

```sql
CREATE DATABASE kubedeck CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'kubedeck'@'%' IDENTIFIED BY '<强随机密码>';
GRANT ALL PRIVILEGES ON kubedeck.* TO 'kubedeck'@'%';
SET GLOBAL log_bin_trust_function_creators = 1;
```

`log_bin_trust_function_creators` 也可以在参数组或 my.cnf 中持久化；未开启时迁移会在创建审计触发器时失败。

2. 编辑 `docker-compose.yml`，把 `KUBEDECK_DATABASE_URL` 改为外部地址，并显式设置驱动：

```yaml
      KUBEDECK_DB_DRIVER: mysql
      KUBEDECK_DATABASE_URL: mysql://kubedeck:<password>@mysql.example.com:3306/kubedeck
```

3. 停止并移除内置数据库服务（或保留但不再连接），然后 `docker compose up -d`。

密码中避免 `@ : / ? #` 等 URL 保留字符；必须使用时做 URL 编码。切换引擎视为新库初始化，不提供跨引擎数据迁移，需要重新登记集群与权限。

## 8. 升级与回滚

```bash
# 生产使用不可变 tag
KUBEDECK_IMAGE=registry.example.com/kubedeck:0.2.0 docker compose up -d

# 本地源码构建
docker compose up -d --build
```

单实例升级会有短暂中断。回滚时把 `KUBEDECK_IMAGE` 切回上一个 tag 并重新 `up -d`。数据库迁移只向前执行且带 checksum：已执行文件被修改会启动失败；如果新版本执行了不兼容迁移，需要同时从备份恢复数据库。

## 9. 内网 / 离线镜像传输

目标机器不能访问镜像仓库时，在联网构建机导出镜像，再导入目标机：

```bash
# 联网机
docker build -t kubedeck:0.2.0 .
docker save kubedeck:0.2.0 | gzip > kubedeck-0.2.0.tar.gz

# 目标机
gunzip -c kubedeck-0.2.0.tar.gz | docker load
```

目标机还需要 `postgres:17-alpine`；可在联网机或用内网 registry 同步。导入后把 `.env` 中的 `KUBEDECK_IMAGE` 设为对应 tag，再 `docker compose up -d --no-build`。目标机没有公网时，OIDC IdP 与镜像仓库都应在内网可达，否则登录和拉取会失败。

## 10. 目标集群网络（WSL2 / Docker Desktop）

容器里的 `127.0.0.1` 是容器自己，不是宿主机，也不是 WSL。若 k3s 跑在 WSL2、Docker 跑在 Windows Docker Desktop，容器默认无法通过 `127.0.0.1:6443` 访问 k3s：

1. **推荐：把 Compose 跑在 WSL2 内**，使用默认 bridge 网络，登记 `server: https://<WSL eth0 IP>:6443`。可用下面的命令确认证书 SAN：
   ```bash
   openssl s_client -connect <WSL-IP>:6443 -servername <WSL-IP> </dev/null 2>/dev/null \
     | openssl x509 -noout -text | grep -A1 'Subject Alternative Name'
   ```
2. **Compose 跑在 Windows Docker Desktop**：`host.docker.internal` 指向 Windows 而不是 WSL，优先尝试 WSL 的 eth0 IP；不可路由时把 apiserver 暴露到容器可达的地址。
3. **仅本地临时验证**：可使用 `insecureSkipTLSVerify: true`，生产不要使用。

排查连通性：

```bash
docker run --rm curlimages/curl -vk https://<可达地址>:6443/version
```

## 11. 故障排查

| 现象 | 排查 |
|---|---|
| `db` 一直 unhealthy | `docker compose logs db`；确认密码与卷中的旧数据一致，旧卷不会因改 `.env` 自动改密码 |
| 应用反复重启 | `docker compose logs kubedeck`；检查主密钥是否为 32 字节 base64、数据库 URL、OIDC 是否可达 |
| `/readyz` 返回 503 | 数据库连接失败或迁移未完成；先看 `db` 健康状态 |
| 登录后立即掉线 | 是否通过 HTTPS 访问；`KUBEDECK_COOKIE_INSECURE=false` 时 Secure Cookie 不会在 HTTP 下发送 |
| OIDC 回调报错 | `KUBEDECK_OIDC_REDIRECT_URL` 与 IdP 登记值是否完全一致；issuer 是否匹配 |
| 实时推送 / 日志卡顿 | 反向代理是否关闭了响应缓冲并透传 WebSocket |
| 无法连接目标集群 | 用 §10 的临时容器测试；检查证书 SAN、网络可达性与 apiserver 监听地址 |
| MySQL 启动报触发器权限错误 | 确认 `log_bin_trust_function_creators=1`，或让迁移账号具备 SUPER（不推荐） |

## 12. 安全清单

- 主密钥、数据库密码、OIDC client secret 只存在于 `.env` 或外部 Secret 管理系统中，不写入镜像与仓库。
- `.env` 权限保持 `0600`；环境变量可被 `docker inspect` 和拥有 Docker 权限的用户读取，生产更严格时应使用外部 Secret 管理系统。
- 目标集群平台 SA 只有 `impersonate`，没有 `cluster-admin`。
- 生产 `KUBEDECK_COOKIE_INSECURE=false`，入口必须 HTTPS。
- 不要在生产启用开发模式；开发模式跳过 OIDC 和权限校验。
- 确认 IdP 返回 `email_verified=true`；只有受控例外才设置 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true`。
- 固定 `replicaCount=1` 的实际形态：不要扩容，升级会有短暂中断。
- 不要对公网映射 8080、5432 或 3306；`/metrics` 只允许内网抓取。
- 定期检查审计哈希链与备份恢复演练。

## 13. 相关文档

- [operations.md](./operations.md)：日常巡检、健康检查、日志与指标
- [backup-restore.md](./backup-restore.md)：备份、恢复与主密钥保管
- [permission-matrix.md](./permission-matrix.md)：Impersonation 与角色映射
- [local-environment.md](./local-environment.md)：本地开发与 k3s 验收环境