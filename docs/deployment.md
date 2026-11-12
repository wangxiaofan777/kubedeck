# 部署手册

本文说明如何在生产环境部署 KubeDeck。命令默认在能访问目标 Kubernetes 集群的机器上执行，Helm 3.16+、kubectl 1.34+。

> 本地开发与验收环境见 [local-environment.md](./local-environment.md)。不要把本文中的占位符替换成真实凭据后提交到仓库。

## 1. 前置条件

| 组件 | 要求 |
|---|---|
| Kubernetes | 1.34–1.37（低于 1.34 拒绝接入） |
| PostgreSQL | 15+，数据库 `kubedeck`，独立账号 |
| OIDC IdP | 支持授权码 + PKCE，能提供 `sub`、`email`、`groups` |
| 入口 | HTTPS（生产 Cookie 必须 Secure） |
| 镜像 | 由 `Dockerfile` 构建并推送到私有仓库 |

KubeDeck 不把集群资源状态写入 PostgreSQL；数据库只保存平台元数据、角色绑定与审计。请为数据库配置备份与保留策略。

## 2. 准备数据库

```sql
CREATE ROLE kubedeck LOGIN PASSWORD '<强随机密码>';
CREATE DATABASE kubedeck OWNER kubedeck;
```

迁移由进程启动时自动执行，不需要单独跑 job。首次启动会创建审计分区。应用账号需要在该库内建表、建分区的权限。

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

必填配置：

| 值 | 说明 |
|---|---|
| `config.oidc.issuer` | IdP issuer，必须与 discovery 返回一致 |
| `config.oidc.clientId` | client id |
| `config.oidc.redirectUrl` | 上面的回调地址 |
| `secrets.oidcClientSecret` | client secret |
| `config.allowedOrigins` | 仅跨域部署时填写，逗号分隔；不要使用 `*` |

## 5. 准备 Secret

推荐用外部 Secret 管理系统创建，chart 只引用：

```bash
kubectl -n kubedeck-system create secret generic kubedeck-secrets \
  --from-literal=KUBEDECK_DATABASE_URL='postgres://kubedeck:<password>@pg.example.com:5432/kubedeck?sslmode=require' \
  --from-literal=KUBEDECK_MASTER_KEY='<base64-32-bytes>' \
  --from-literal=KUBEDECK_OIDC_CLIENT_SECRET='<client-secret>'
```

然后 `--set secrets.existingSecret=kubedeck-secrets`。如果不用外部 Secret，也可以设置 `secrets.databaseUrl`、`secrets.masterKey`、`secrets.oidcClientSecret`，由 chart 创建 Secret；这种方式会让值进入 Helm release 元数据，生产环境不推荐。

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

`networkPolicy.enabled=true` 时默认只放行 DNS 与 5432/6443/443 出站；入站规则为空表示拒绝所有入站，请按入口控制器补充 `networkPolicy.ingress`。

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
- Ingress 只暴露 443；数据库不对公网开放。
- 定期检查审计哈希链与备份恢复演练。