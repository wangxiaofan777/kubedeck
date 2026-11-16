# 离线部署（Air-gapped）

本文说明如何在**不能访问公网**的 Linux 服务器上部署 KubeDeck。构建和打包在联网机器完成，目标机只做校验、导入和启动。

> 与 [deployment.md](deployment.md)、[deploy/compose/README.md](../deploy/compose/README.md) 的关系：那两份文档默认目标机可以拉取镜像、访问 ACME 和公网 OIDC；本文只覆盖离线差异，其余配置项含义不变。

## 0. 先确认边界

KubeDeck 运行时**不访问公网**：前端、Monaco 编辑器、数据库迁移都打包在二进制里，没有 CDN、字体或遥测请求。离线部署仍需要以下组件在目标网络内可达：

| 依赖 | 是否在离线包 | 说明 |
|---|---|---|
| Kubernetes 1.18+（Helm 方式；1.18–1.20 为 legacy）或 Docker 24+ / Compose v2.24+（Compose 方式） | 否 | 目标环境自行准备 |
| PostgreSQL 15+ 或 MySQL 8.0.20+ | Compose 包内含 PostgreSQL；MySQL 需加 `--with-mysql`；Helm 方式需外部实例 | 应用启动时自动迁移 |
| OIDC IdP（Keycloak/Dex 等） | 否 | **硬依赖**，启动时 discovery 失败会直接退出 |
| 内网 TLS 证书或内部 CA | 部分 | Compose 默认使用 Caddy 内部 CA；Helm 由 Ingress 终止 TLS |
| 目标集群 apiserver | 否 | 必须从 KubeDeck 容器/Pod 网络可达 |

`dev.enabled` 会跳过 OIDC 与权限校验，**不能**当作离线认证方案用于生产。

## 1. 镜像体积与交付物

实测 `linux/amd64`、v0.1.0：

| 镜像 | 未压缩 | gzip 后 |
|---|---|---|
| `kubedeck:0.1.0` | 68.2 MB | 16.4 MB |
| `postgres:17-alpine` | 424 MB | 116 MB |
| `caddy:2-alpine` | 92 MB | 25 MB |

- **Helm 离线包**（`--no-deps`）只有 KubeDeck 镜像，传输约 **17 MB**。
- **Compose 完整包**包含三个镜像，传输约 **157 MB**，解压后占盘约 590 MB。
- 需要随包携带 MySQL 8 时加 `--with-mysql`，会额外包含 `mysql:8.0` 镜像（体积以实际镜像为准），并附带 `docker-compose.mysql.yml` 与 `env.mysql.example`。

`docker save` 输出的是未压缩 tar，因此脚本统一用 `gzip -9` 压缩；`docker load` 与 `ctr images import` 都能直接读取 `.tar.gz`。

打包完成后目录结构：

```text
dist/offline/
├── kubedeck-0.1.0-linux-amd64.tar.gz
├── postgres-17-alpine-linux-amd64.tar.gz      # --no-deps 时没有
├── caddy-2-alpine-linux-amd64.tar.gz          # --no-deps 时没有
├── mysql-8.0-linux-amd64.tar.gz               # 仅 --with-mysql 时生成
├── kubedeck-chart-0.1.0.tar.gz                # Helm Chart
├── docker-compose.yml
├── docker-compose.mysql.yml                   # MySQL 8 变体
├── docker-compose.offline.yml
├── Caddyfile.offline
├── env.example
├── env.mysql.example                          # MySQL 8 变体
├── helm-values.yaml                           # 参考 values，按需修改
├── offline-deployment.md                      # 本文副本
├── load-images.sh
├── push-images.sh                             # 传 --registry 时生成
├── manifest.txt                               # 版本、平台、Git commit、镜像 ID
└── SHA256SUMS
```

包里**不包含**任何密钥、`.env` 或 kubeconfig。

## 2. 联网机：构建与打包

前置条件：Docker 24+（含 buildx）、git、gzip、sha256sum；能访问 Docker Hub、npm registry 和 Go 模块代理。Windows 建议在 WSL2 内执行。

```bash
git checkout <发布 tag>
bash scripts/offline-bundle.sh --version 0.1.0
```

默认构建 `linux/amd64`，输出到 `dist/offline/`。常用变体：

```bash
# ARM 服务器（鲲鹏、飞腾、Graviton 等）
bash scripts/offline-bundle.sh --version 0.1.0 --platform linux/arm64

# 只部署 Helm，数据库和 IdP 已存在：只导应用镜像
bash scripts/offline-bundle.sh --version 0.1.0 --no-deps

# Compose 使用 MySQL 8：额外打包 MySQL 镜像
bash scripts/offline-bundle.sh --version 0.1.0 --with-mysql

# 多节点走内网 registry：额外生成推送脚本
bash scripts/offline-bundle.sh --version 0.1.0 --registry harbor.example.com/kubedeck

# 复用本地已构建的镜像，不重新编译
bash scripts/offline-bundle.sh --version 0.1.0 --no-build
```

脚本行为：

1. `docker buildx build --platform <platform> --load` 构建镜像（禁用 provenance/SBOM，避免导出额外 manifest）；
2. 逐个 `docker save` 后 gzip，并写入 `SHA256SUMS`；
3. 复制 Compose 文件、离线覆盖文件、Caddy 配置、Helm Chart 和本文；
4. 生成 `manifest.txt`、`load-images.sh`，可选 `push-images.sh`。

一次只产出一个平台。需要同时支持 amd64/arm64 时执行两次，输出目录用 `--out` 分开。

## 3. 传输与校验

```bash
# 传输（任选其一）
rsync -av dist/offline/ ops@target:/opt/kubedeck-offline/
scp -r dist/offline ops@target:/opt/kubedeck-offline/

# 目标机校验
cd /opt/kubedeck-offline
sha256sum -c SHA256SUMS
```

先在目标机核对 `manifest.txt` 里的版本、平台、Git commit，再导入。校验失败不要继续。

## 4. 目标机：导入镜像

### 4.1 Docker / Docker Compose

```bash
cd /opt/kubedeck-offline
./load-images.sh

# 或手工逐个导入
docker load -i kubedeck-0.1.0-linux-amd64.tar.gz
docker load -i postgres-17-alpine-linux-amd64.tar.gz
docker load -i caddy-2-alpine-linux-amd64.tar.gz
# 使用 MySQL 8 时：
# docker load -i mysql-8.0-linux-amd64.tar.gz

docker images | grep -E 'kubedeck|postgres|mysql|caddy'
```

`load-images.sh` 会先执行 `sha256sum -c`，再加载所有镜像。

### 4.2 containerd（Kubernetes 节点）

镜像必须存在于**每一个可能调度到 KubeDeck Pod 的节点**。

```bash
# 普通 containerd
sudo ctr -n k8s.io images import kubedeck-0.1.0-linux-amd64.tar.gz

# k3s
sudo k3s ctr images import kubedeck-0.1.0-linux-amd64.tar.gz

# 验证
sudo crictl images | grep kubedeck
```

若节点是 Docker runtime（已不推荐）：

```bash
sudo docker load -i kubedeck-0.1.0-linux-amd64.tar.gz
```

导入后的镜像名是 `docker.io/library/kubedeck:0.1.0`。Chart 里保持 `repository: kubedeck` 即可匹配；若写了 `kubedeck:0.1.0` 作为 repository 会变成 `kubedeck:0.1.0:0.1.0`，注意不要写错。

### 4.3 内网 registry（多节点推荐）

单节点导入最简单；多节点时逐台导入容易漏节点，建议推送到内网 Harbor/registry：

```bash
# 打包时带 --registry 会生成 push-images.sh
./push-images.sh

# 然后在 values 中指定
# image:
#   repository: harbor.example.com/kubedeck/kubedeck
#   tag: "0.1.0"
#   pullPolicy: IfNotPresent
```

需要认证时创建 `imagePullSecrets` 并设置 `imagePullSecrets` 或 `--set imagePullSecrets[0].name=...`。Compose 方式把 `.env` 中的 `KUBEDECK_IMAGE` 改为 registry 地址，并先 `docker login`。

## 5. Compose 离线部署

适用于单机、小团队、边缘环境。应用固定单副本。

要求 Docker Compose v2.24+（离线覆盖文件用 `!reset` 移除基础文件里的 `build` 段）。更早的 Compose 不支持该合并标签，升级 Compose 插件或手工删除 `build` 段。

```bash
cd /opt/kubedeck-offline
cp env.example .env
chmod 600 .env
vi .env
```

`.env` 至少修改：

```dotenv
KUBEDECK_IMAGE=kubedeck:0.1.0
KUBEDECK_DOMAIN=kubedeck.internal.example.com
POSTGRES_PASSWORD=<强随机密码>
KUBEDECK_MASTER_KEY=<openssl rand -base64 32>
KUBEDECK_OIDC_ISSUER=https://idp.internal.example.com/realms/kubedeck
KUBEDECK_OIDC_CLIENT_ID=kubedeck
KUBEDECK_OIDC_CLIENT_SECRET=<client secret>
KUBEDECK_OIDC_REDIRECT_URL=https://kubedeck.internal.example.com/auth/callback
KUBEDECK_BOOTSTRAP_TOKEN=<至少 16 位随机串>
```

启动：

```bash
docker compose -f docker-compose.yml -f docker-compose.offline.yml up -d --no-build
docker compose ps
docker compose logs -f kubedeck
```

两个关键参数缺一不可：

- `-f docker-compose.offline.yml`：设置 `pull_policy: never`、去掉 `build`、把 Caddy 换成内部 CA 配置；
- `--no-build`：即使镜像缺失也只报错，不会尝试联网构建。

如果没有 `--no-build` 且本地镜像被误删，Compose 可能尝试 `build` 并卡在拉取基础镜像。

### 5.1 内网 TLS

离线覆盖文件使用 `Caddyfile.offline`，默认 `tls internal`：

- Caddy 自动生成内部 CA，不需要域名验证；
- 首次访问浏览器会告警，需要导入根证书：

```bash
docker compose -f docker-compose.yml -f docker-compose.offline.yml \
  exec proxy cat /data/caddy/pki/authorities/local/root.crt > kubedeck-root.crt
```

把 `kubedeck-root.crt` 导入客户端浏览器；Linux 服务器可执行：

```bash
sudo cp kubedeck-root.crt /usr/local/share/ca-certificates/kubedeck-root.crt
sudo update-ca-certificates
```

如果已有企业 CA，直接使用自己的证书：

```bash
mkdir -p certs
cp fullchain.pem certs/tls.crt
cp privkey.pem certs/tls.key
# 编辑 Caddyfile.offline：把 `tls internal` 替换为
# tls /etc/caddy/certs/tls.crt /etc/caddy/certs/tls.key
```

`docker-compose.offline.yml` 已把 `./certs` 挂载到 `/etc/caddy/certs`。

如果前面已有企业网关，不需要 Caddy：

```bash
docker compose -f docker-compose.yml -f docker-compose.offline.yml up -d --no-build kubedeck db
```

此时应用只在内部网络暴露 8080，需要让网关能访问该网络，或另加一个端口映射覆盖文件。

### 5.2 内网 OIDC 使用私有 CA

若 IdP 证书由内网 CA 签发，容器默认不信任。追加一个覆盖文件 `docker-compose.ca.yml`：

```yaml
services:
  kubedeck:
    environment:
      SSL_CERT_FILE: /etc/kubedeck/ca/internal-ca.crt
    volumes:
      - ./internal-ca.crt:/etc/kubedeck/ca/internal-ca.crt:ro
```

```bash
docker compose -f docker-compose.yml -f docker-compose.offline.yml \
  -f docker-compose.ca.yml up -d --no-build
```

`SSL_CERT_FILE` 会替代镜像自带的根证书集合；离线环境通常不需要公网根证书。如果 IdP 使用中间证书，把完整链写进同一个文件。

## 6. Helm 离线部署

前置条件：Kubernetes 1.18–1.37（1.18–1.20 为 legacy 档）、Helm 3.16+、外部 PostgreSQL 15+ 或 MySQL 8.0.20+、已安装的 Ingress Controller、可用的 OIDC IdP。Helm 与 kubectl 二进制需要提前拷入目标机；Chart 没有外部依赖，`helm install` 不会联网。

### 6.1 导入镜像

按 §4.2 在每个节点导入，或按 §4.3 推送到内网 registry。

### 6.2 准备 Secret

```bash
kubectl create namespace kubedeck-system

kubectl -n kubedeck-system create secret generic kubedeck-secrets \
  --from-literal=KUBEDECK_DATABASE_URL='postgres://kubedeck:<password>@<pg-host>:5432/kubedeck?sslmode=require' \
  --from-literal=KUBEDECK_MASTER_KEY='<base64-32-bytes>' \
  --from-literal=KUBEDECK_OIDC_CLIENT_SECRET='<client-secret>'
```

MySQL 部署把连接串替换为 `mysql://kubedeck:<password>@<mysql-host>:3306/kubedeck`，并设置 `database.driver=mysql`；MySQL 服务端需开启 `log_bin_trust_function_creators=1`（审计只追加触发器）。

### 6.3 准备 values

```bash
tar -xzf kubedeck-chart-0.1.0.tar.gz
cp helm-values.yaml values-prod.yaml
```

编辑 `values-prod.yaml`，最少包含：

```yaml
replicaCount: 1

image:
  repository: kubedeck        # 预导入节点时；用内网 registry 则写完整地址
  tag: "0.1.0"
  pullPolicy: IfNotPresent    # 确认不会回落到公网 registry

config:
  oidc:
    issuer: https://idp.internal.example.com/realms/kubedeck
    clientId: kubedeck
    redirectUrl: https://kubedeck.internal.example.com/auth/callback
  cookieInsecure: false

secrets:
  existingSecret: kubedeck-secrets

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: kubedeck.internal.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: kubedeck-tls
      hosts:
        - kubedeck.internal.example.com
```

证书由企业 CA 或 Ingress 提供：

```bash
kubectl -n kubedeck-system create secret tls kubedeck-tls \
  --cert=fullchain.pem --key=privkey.pem
```

### 6.4 内网 CA 信任

Chart 支持 `extraEnv`、`extraVolumes`、`extraVolumeMounts`。把 CA 做成 ConfigMap：

```bash
kubectl -n kubedeck-system create configmap kubedeck-internal-ca \
  --from-file=internal-ca.crt
```

`values-prod.yaml` 追加：

```yaml
extraEnv:
  - name: SSL_CERT_FILE
    value: /etc/kubedeck/ca/internal-ca.crt
extraVolumes:
  - name: internal-ca
    configMap:
      name: kubedeck-internal-ca
extraVolumeMounts:
  - name: internal-ca
    mountPath: /etc/kubedeck/ca
    readOnly: true
```

### 6.5 渲染并安装

```bash
helm template kubedeck ./kubedeck -n kubedeck-system -f values-prod.yaml \
  > /tmp/kubedeck.yaml
kubectl apply --dry-run=server -f /tmp/kubedeck.yaml

helm upgrade --install kubedeck ./kubedeck \
  -n kubedeck-system \
  -f values-prod.yaml \
  --wait --timeout 5m

kubectl -n kubedeck-system get pods
kubectl -n kubedeck-system logs deploy/kubedeck
```

`replicaCount` 只能为 1；大于 1 时 Chart 会直接渲染失败（会话保存在进程内存）。

## 7. 首次登录与接入集群

两种方式一致：

1. 浏览器打开 KubeDeck 地址，完成 OIDC 登录；
2. 用引导令牌创建首个平台管理员：

```bash
curl -X POST https://kubedeck.internal.example.com/auth/bootstrap \
  -H "X-Bootstrap-Token: $KUBEDECK_BOOTSTRAP_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"subject":"<OIDC sub>","email":"admin@example.com","displayName":"平台管理员"}'
```

成功后再次调用返回 409，引导令牌自动失效。

3. 按 [deployment.md §8](deployment.md) 在每个被管理集群创建只授予 `impersonate` 的 ServiceAccount，然后登记集群：

```bash
curl -X POST https://kubedeck.internal.example.com/api/v1/clusters \
  -H 'Content-Type: application/json' \
  -b 'kubedeck_session=<浏览器 Cookie>' \
  -d '{"name":"prod","displayName":"生产","server":"https://10.0.0.10:6443","token":"<SA Token>","caData":"<PEM>"}'
```

平台只接受 `https://` apiserver；内网私有 CA 通过请求里的 `caData` 提供，不依赖宿主机信任链。

## 8. 离线 OIDC 要点

- **Issuer 必须一致**：`KUBEDECK_OIDC_ISSUER` 必须与 IdP discovery 返回的 `issuer` 完全相同，浏览器和 KubeDeck 都要能访问该地址。
- **回调地址**：`KUBEDECK_OIDC_REDIRECT_URL` 必须与 IdP 中登记值逐字符一致。
- **groups claim**：KubeDeck 用 `groups` 做 Impersonate-Group。Keycloak 需要添加 Group Membership mapper，claim 名设为 `groups`。
- **email_verified**：IdP 返回 `email_verified=false` 或缺失时登录会被拒绝。只有确认邮箱可信后，才设置 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true`，并记录为例外。
- **IdP 自身高可用**：IdP 不可达时 KubeDeck 启动失败，这是有意为之；请把 IdP 纳入同一套运维与备份。
- **时间同步**：内网机器必须 NTP 对时，否则 JWT 校验会失败。

## 9. 升级与回滚

```bash
# 联网机：构建新版本并打包
bash scripts/offline-bundle.sh --version 0.2.0

# 目标机：校验、导入
sha256sum -c SHA256SUMS
./load-images.sh
```

Compose：

```bash
# 先备份数据库（PostgreSQL）
docker compose exec -T db pg_dump -U kubedeck -Fc kubedeck > kubedeck-$(date +%F).dump

# MySQL 部署改用：
# docker compose -f docker-compose.mysql.yml exec -T db \
#   mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction --routines --triggers kubedeck > kubedeck-$(date +%F).sql

sed -i 's/^KUBEDECK_IMAGE=.*/KUBEDECK_IMAGE=kubedeck:0.2.0/' .env
docker compose -f docker-compose.yml -f docker-compose.offline.yml up -d --no-build
```

Helm：

```bash
helm upgrade kubedeck ./kubedeck -n kubedeck-system -f values-prod.yaml \
  --set image.tag=0.2.0 --wait
```

注意：

- 单副本升级有短暂中断；
- 数据库迁移只向前执行且带 checksum，回滚镜像前先确认旧版本兼容当前 schema；跨大版本回滚请先恢复数据库备份；
- 验证完成前保留上一版镜像包和 `manifest.txt`。

## 10. 验收清单

| 项 | 命令 / 判据 |
|---|---|
| 包完整性 | `sha256sum -c SHA256SUMS` 全部 OK |
| 镜像已导入 | `docker images` 或 `crictl images` 能看到对应 tag |
| 应用健康 | `curl --cacert kubedeck-root.crt https://<域名>/healthz` 返回 200 |
| 数据库就绪 | `/readyz` 返回 200；否则先看数据库（PostgreSQL/MySQL） |
| OIDC 登录 | 浏览器完成授权码登录，无 `email_verified` 报错 |
| 管理员引导 | `/auth/bootstrap` 首次 201、再次 409 |
| 集群接入 | 登记后能浏览命名空间与 Pod |
| 写操作审计 | 执行一次编辑/伸缩，审计页出现记录 |
| 重启持久化 | `docker compose restart` 或滚动重启后数据仍在 |
| 指标隔离 | `/metrics` 未暴露到公网，仅内网可抓取 |

## 11. 常见问题

| 现象 | 原因与处理 |
|---|---|
| `pull access denied` / `failed to resolve reference` | 没加 `-f docker-compose.offline.yml` 或镜像 tag 与 `KUBEDECK_IMAGE` 不一致；确认已 `docker load` |
| Compose 仍尝试构建 | 漏了 `--no-build`，或没使用离线覆盖文件（`build` 段未被移除） |
| `exec format error` | 镜像架构与主机不符；用 `--platform linux/arm64` 重新打包 |
| `ImagePullBackOff` | 镜像只导入到部分节点；改为每节点导入或使用内网 registry；检查 `pullPolicy` |
| `x509: certificate signed by unknown authority` | IdP/数据库使用私有 CA；设置 `SSL_CERT_FILE` 并挂载 CA（见 §5.2、§6.4） |
| 浏览器提示证书不受信任 | Compose 使用内部 CA，需导入 `kubedeck-root.crt`；或换成企业证书 |
| OIDC discovery 失败 | IdP 地址从容器/Pod 不可达，或 issuer 与 discovery 返回不一致 |
| 登录后立即掉线 | 通过 HTTP 访问但 `cookieInsecure=false`；必须走 HTTPS |
| `helm` 报 Kubernetes 版本不满足 | Chart 要求 1.18+；升级集群或使用 Compose 方式 |
| `docker load` 报磁盘不足 | 完整包解压需要约 590 MB（含 PostgreSQL 镜像），预留 2 GB 更稳妥；带 `--with-mysql` 时再增加 MySQL 镜像体积 |
| `replicaCount must be 1` | 会话在进程内存，离线也不能扩副本 |

## 12. 安全注意事项

- 离线包只含镜像和部署文件，**不要把 `.env`、kubeconfig、主密钥打进包**。
- `SHA256SUMS` 和 `manifest.txt` 随包传输；有条件时在传输前后各校验一次。
- 使用不可变版本号，不要用 `latest`；升级前保留旧包。
- 主密钥与数据库备份分开保管；丢失主密钥后已登记的集群凭据无法解密。
- 内网 registry 需要认证时使用 `imagePullSecrets`，不要把凭据写进 values。
- `/metrics` 无鉴权，只允许内网抓取；PostgreSQL 端口不要暴露到业务网。