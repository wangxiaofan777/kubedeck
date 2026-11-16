# 部署手册

本文说明如何在生产环境部署 KubeDeck。命令默认在能访问目标 Kubernetes 集群的机器上执行，Helm 3.16+、kubectl 1.34+。

> 本地开发与验收环境见 [local-environment.md](./local-environment.md)。不要把本文中的占位符替换成真实凭据后提交到仓库。

## 非 Kubernetes 部署（Docker Compose）

如果目标环境没有 Kubernetes，使用 [`deploy/compose/`](../deploy/compose/README.md)：KubeDeck + PostgreSQL（或 MySQL 8）+ Caddy，一条 `docker compose up -d --build` 启动。

适用与边界：

- 适合单机、小团队、演示和边缘环境。
- 应用固定 **单副本**；会话在进程内存、后台任务无选主，不要横向扩容。
- 目标集群仍需通过 kubeconfig 或 `server + token + CA` 登记，平台到 apiserver 必须网络可达。
- 多副本、零停机升级和高可用仍使用本文的 Helm 方案。

Compose 的密钥、备份、WSL/Docker Desktop 网络与故障排查见 [Compose 部署说明](../deploy/compose/README.md)。

## 离线部署

目标机器不能访问公网（Docker Hub、npm、Go 模块代理、ACME）时，不要直接在目标机执行本文的 `docker build` 或 `helm install --set image.repository=...`。先在联网机用 [`scripts/offline-bundle.sh`](../scripts/offline-bundle.sh) 构建并导出镜像，再在目标机校验、导入后按本文同样的配置安装。

应用镜像约 68 MB（gzip 后约 16 MB），Helm 场景只需传输这一份；Compose 完整包还包含 PostgreSQL 与 Caddy，约 157 MB（需要随包携带 MySQL 8 时使用 `--with-mysql`）。镜像导入、containerd/内网 registry、内部 CA 与离线 OIDC 的完整步骤见 [offline-deployment.md](offline-deployment.md)。

## 1. 前置条件

| 组件 | 要求 |
|---|---|
| Kubernetes | 1.34–1.37（低于 1.34 拒绝接入） |
| 数据库 | PostgreSQL 15+（默认）或 MySQL 8.0.20+，数据库 `kubedeck`，独立账号 |
| OIDC IdP | 支持授权码 + PKCE，能提供 `sub`、`email`、`groups` |
| 入口 | HTTPS（生产 Cookie 必须 Secure） |
| 镜像 | 由 `Dockerfile` 构建并推送到私有仓库 |

KubeDeck 不把集群资源状态写入数据库；数据库只保存平台元数据、角色绑定与审计。请为数据库配置备份与保留策略。

引擎通过 `KUBEDECK_DATABASE_URL` 的协议前缀识别（`postgres://` / `mysql://`），也可以显式设置 `KUBEDECK_DB_DRIVER=postgres|mysql`；两者冲突或驱动不受支持时进程拒绝启动。切换引擎视为新库初始化，不提供跨引擎数据迁移。

## 2. 准备数据库

PostgreSQL：

```sql
CREATE ROLE kubedeck LOGIN PASSWORD '<强随机密码>';
CREATE DATABASE kubedeck OWNER kubedeck;
```

MySQL 8：

```sql
CREATE DATABASE kubedeck CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'kubedeck'@'%' IDENTIFIED BY '<强随机密码>';
GRANT ALL PRIVILEGES ON kubedeck.* TO 'kubedeck'@'%';
-- 审计只追加依赖触发器；开启 binlog 时必须由管理员设置：
SET GLOBAL log_bin_trust_function_creators = 1;
```

MySQL 的 `log_bin_trust_function_creators` 也可以在参数组/my.cnf 中持久化。未开启时，迁移会在创建审计触发器时失败并给出明确提示。

MySQL 长期停机超过预创建窗口（当前 3 个月）时，新写入的审计行会进入 `audit_logs_max` 兜底分区；保留策略不会删除兜底分区，恢复后请按需手工拆分/归档。正常运行时启动与每日维护会补齐未来月份分区。

迁移由进程启动时自动执行，不需要单独跑 job。首次启动会创建审计分区。应用账号需要在该库内建表、建触发器、建分区的权限。MySQL 的 DDL 会隐式提交，迁移文件按幂等方式编写，失败后重跑即可继续。

## 3. 生成主密钥

主密钥用于 AES-256-GCM 加密集群凭据，**不能入库、不能提交仓库**。推荐存放在 Vault/KMS，再由 External Secrets 注入 Secret。

```bash
openssl rand -base64 32
```

得到 32 字节的 base64 字符串，作为 `KUBEDECK_MASTER_KEY`。`KUBEDECK_MASTER_KEY_VERSION` 从 `1` 开始，每次轮换递增。

### 轮换主密钥

1. 生成新密钥，记版本 `N+1`。
2. 更新 Secret：`KUBEDECK_PREVIOUS_MASTER_KEY=旧密钥`、`KUBEDECK_PREVIOUS_MASTER_KEY_VERSION=N`、`KUBEDECK_MASTER_KEY=新密钥`、`KUBEDECK_MASTER_KEY_VERSION=N+1`。
3. 滚动重启 Deployment。新旧密文都能解密，新写入使用新密钥。
4. 对每个已登记集群调用凭据轮换接口（`PUT /api/v1/clusters/{cluster}/credential`），用同一份 kubeconfig 重新加密，确保旧密钥不再被引用。
5. 确认没有 `key_version=N` 的凭据后，从 Secret 删除 `KUBEDECK_PREVIOUS_MASTER_KEY*` 并再次滚动重启。

轮换中断可以安全重试：解密优先尝试当前版本，失败再尝试上一版本。

## 4. 配置 OIDC

在 IdP 创建 confidential client：

- Redirect URI：`https://kubedeck.example.com/auth/callback`
- Scopes：`openid profile email groups`
- 需要把用户所属组映射到 `groups` claim（KubeDeck 用它做 Impersonate-Group）。
- ID Token 携带 `email` 时，默认要求 `email_verified=true`；否则登录会被拒绝，避免把未验证邮箱当作集群身份。

`groups` claim 会先去空白、去重并限制数量（最多 64 个，单个最多 256 字符），异常值不会进入 impersonation。

必填配置：

| 值 | 说明 |
|---|---|
| `config.oidc.issuer` | IdP issuer，必须与 discovery 返回一致 |
| `config.oidc.clientId` | client id |
| `config.oidc.redirectUrl` | 上面的回调地址 |
| `secrets.oidcClientSecret` | client secret |
| `config.allowedOrigins` | 仅跨域部署时填写，逗号分隔；不要使用 `*` |

如果 IdP 不返回 `email_verified`，登录会失败。确认该 IdP 的邮箱可信后，可在 Secret/环境变量中显式设置 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true` 放宽；这是受控例外，建议在安全评审中记录原因。不要把它当作默认值。

## 5. 准备 Secret

推荐用外部 Secret 管理系统创建，chart 只引用：

```bash
kubectl -n kubedeck-system create secret generic kubedeck-secrets \
  --from-literal=KUBEDECK_DATABASE_URL='postgres://kubedeck:<password>@pg.example.com:5432/kubedeck?sslmode=require' \
  --from-literal=KUBEDECK_MASTER_KEY='<base64-32-bytes>' \
  --from-literal=KUBEDECK_OIDC_CLIENT_SECRET='<client-secret>'
```

然后 `--set secrets.existingSecret=kubedeck-secrets`。如果不用外部 Secret，也可以设置 `secrets.databaseUrl`、`secrets.masterKey`、`secrets.oidcClientSecret`，由 chart 创建 Secret；这种方式会让值进入 Helm release 元数据，生产环境不推荐。

MySQL 连接串示例（同时设置 `database.driver=mysql`）：

```bash
--from-literal=KUBEDECK_DATABASE_URL='mysql://kubedeck:<password>@mysql.example.com:3306/kubedeck'
```

MySQL 密码中避免 `@ : / ? #` 等 URL 保留字符；如必须使用，请做 URL 编码。也可以直接使用驱动原生 DSN 并显式设置 `KUBEDECK_DB_DRIVER=mysql`。

## 6. 安装

先渲染检查：

```bash
helm template kubedeck deploy/helm/kubedeck \
  --namespace kubedeck-system \
  --kube-version 1.37.0 \
  --set secrets.existingSecret=kubedeck-secrets \
  --set config.oidc.issuer=https://idp.example.com \
  --set config.oidc.clientId=kubedeck \
  --set config.oidc.redirectUrl=https://kubedeck.example.com/auth/callback \
  >/tmp/kubedeck.yaml

kubectl apply --dry-run=server -f /tmp/kubedeck.yaml
```

当前会话与 OIDC 访问令牌只保存在进程内存中，因此 chart 强制 `replicaCount: 1`；设置为大于 1 时 `helm template`/`helm install` 会直接失败并说明原因。需要多副本前必须先实现共享会话存储。

安装：

```bash
helm upgrade --install kubedeck deploy/helm/kubedeck \
  --namespace kubedeck-system --create-namespace \
  --set image.repository=registry.example.com/kubedeck \
  --set image.tag=0.1.0 \
  --set secrets.existingSecret=kubedeck-secrets \
  --set config.oidc.issuer=https://idp.example.com \
  --set config.oidc.clientId=kubedeck \
  --set config.oidc.redirectUrl=https://kubedeck.example.com/auth/callback \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set 'ingress.hosts[0].host=kubedeck.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/' \
  --set 'ingress.hosts[0].paths[0].pathType=Prefix' \
  --set 'ingress.tls[0].secretName=kubedeck-tls' \
  --set 'ingress.tls[0].hosts[0]=kubedeck.example.com' \
  --wait --timeout 5m
```

可选组件：

```bash
--set pdb.enabled=true --set pdb.minAvailable=1
--set autoscaling.enabled=true --set autoscaling.minReplicas=2
--set networkPolicy.enabled=true
--set serviceMonitor.enabled=true   # 需要 Prometheus Operator CRD
```

`networkPolicy.enabled=true` 时默认只放行 DNS 与 5432/6443/443 出站；`database.driver=mysql` 时会额外放行 3306。入站规则为空表示拒绝所有入站，请按入口控制器补充 `networkPolicy.ingress`。

## 7. 创建首个平台管理员

1. 浏览器打开 `https://kubedeck.example.com`，完成一次 OIDC 登录。
2. 用引导令牌创建管理员记录（令牌至少 16 位，建议一次性生成后立即从 Secret 删除）：

```bash
curl -X POST https://kubedeck.example.com/auth/bootstrap \
  -H "X-Bootstrap-Token: $KUBEDECK_BOOTSTRAP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"subject":"<OIDC sub>","email":"admin@example.com","displayName":"平台管理员"}'
```

第一个管理员创建成功后，bootstrap 返回 409，引导令牌不再能创建第二个管理员。之后可在 `/api/v1/admin/users` 查看用户并配置角色绑定。

## 8. 为目标集群创建平台 ServiceAccount

在**每个被管理集群**执行。平台 ServiceAccount 只允许 impersonate，绝不要绑定 `cluster-admin`：

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

生成 kubeconfig（token 有有效期，生产建议用长期 ServiceAccount token Secret 或证书）：

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

## 9. 登记集群

当前版本通过管理 API 登记。先登录控制台取得会话 Cookie（浏览器开发者工具 Network 中的 `kubedeck_session`），再执行：

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

登记时会探测 `/version` 与 discovery，低于 1.34 或无法连通会返回明确错误且不落库。凭据以 AES-256-GCM 加密存储，接口只返回脱敏元数据。

## 10. 升级与回滚

```bash
helm upgrade kubedeck deploy/helm/kubedeck -n kubedeck-system -f values-prod.yaml --wait
helm history kubedeck -n kubedeck-system
helm rollback kubedeck <revision> -n kubedeck-system --wait
```

数据库迁移只向前执行，且带 checksum：已执行文件被修改会启动失败。回滚镜像前先确认旧版本能兼容当前 schema；跨大版本回滚请先从备份恢复数据库。

## 11. 安全清单

- 主密钥、数据库密码、OIDC client secret 只存在于 Secret/Vault，不写入 values 文件或镜像。
- 目标集群平台 SA 只有 `impersonate`，没有 `cluster-admin`。
- 生产 `cookieInsecure=false`，入口必须 HTTPS。
- 不要启用 `dev.enabled`；dev 模式跳过 OIDC 和权限校验。
- 确认 IdP 返回 `email_verified=true`；只有受控例外才设置 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true`。
- 保持 `replicaCount: 1`；会话存储未共享前不要横向扩容。
- Ingress 只暴露 443；数据库不对公网开放。
- 定期检查审计哈希链与备份恢复演练。