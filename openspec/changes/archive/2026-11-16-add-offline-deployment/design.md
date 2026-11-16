## 目标

在不改变应用运行时行为的前提下，让 KubeDeck 能在完全隔离的 Linux 机器上部署：联网机构建并导出，目标机校验、导入、启动。交付物必须可重复、可校验、体积可控。

## 关键决策

### 1. 一次打包，两种部署方式共用一份应用镜像

Compose 与 Helm 使用同一个 `kubedeck:<version>` 镜像，区别只在编排文件。离线包按用途分两级：

| 包 | 内容 | 传输体积（gzip） | 适用 |
|---|---|---|---|
| 应用包 | KubeDeck 镜像、Compose/Helm 文件、校验和 | 约 17 MB | Helm 部署（PostgreSQL 与 IdP 已存在） |
| 完整包 | 应用包 + `postgres:17-alpine` + `caddy:2-alpine` | 约 157 MB | Compose 单机全新部署 |

第三方镜像可按需排除：`--no-deps` 只导应用镜像；`--registry` 额外生成推送脚本。

### 2. 压缩方式：`docker save | gzip`

`docker save` 输出的是未压缩 tar，直接传输会显著放大体积。统一用 `gzip -9`：

- 目标机（Docker、containerd、BusyBox）几乎都自带 `gzip`，`docker load` 和 `ctr images import` 都能直接读 `.tar.gz`；
- `zstd` 压缩率略好但需要目标机预装，离线环境反而增加依赖；
- 镜像在 registry 内本来就是 gzip 层，重新打包后体积与 `docker pull` 相当。

脚本对每个镜像单独生成 `*.tar.gz`，并在 `SHA256SUMS` 中记录校验值，传输后先 `sha256sum -c` 再导入。

### 3. 构建使用 buildx，显式指定平台

`Dockerfile` 的三个阶段（node、golang、distroless）都支持 amd64/arm64。脚本默认 `--platform linux/amd64`，ARM 服务器显式传 `--platform linux/arm64`。一次只产出一个平台，避免 manifest list 在 `docker load` 场景下的兼容问题。

构建机必须联网：需要拉取 `node:24-alpine`、`golang:1.27`、`gcr.io/distroless/static-debian12:nonroot`，并执行 `npm ci` 与 `go mod download`。目标机不需要 Go、Node 或源码。

### 4. 镜像分发：单节点导入，多节点用内网 registry

- **Compose / 单节点**：`docker load -i kubedeck-<version>-linux-amd64.tar.gz`。
- **Kubernetes**：镜像必须存在于每个可能调度到的节点上。节点用 containerd 时 `ctr -n k8s.io images import`；用 Docker 时 `docker load`。多节点更推荐导入一台后 `docker tag` + `docker push` 到内网 registry，再在 chart 中设置 `image.repository` 和 `imagePullSecrets`。
- chart 默认 `pullPolicy: IfNotPresent`；离线预导入场景可改为 `Never`，避免节点意外访问公网 registry。

### 5. 离线 TLS：Caddy 不再走 ACME

`deploy/compose/Caddyfile` 对真实域名使用公共 ACME，隔离环境既无法完成域名验证，也连不上 Let's Encrypt。离线覆盖文件挂载 `Caddyfile.offline`，提供两种模式：

- 默认 `tls internal`：Caddy 自建内部 CA，开机即用；浏览器需安装并信任导出的根证书。
- 已有企业 CA：把证书挂到 `/etc/caddy/certs`，把 `tls internal` 替换为证书路径。

Helm 部署不负责签发证书，由集群 Ingress 或企业网关终止 TLS；文档给出 `kubectl create secret tls` 示例。

### 6. OIDC 仍是硬依赖

应用启动时执行 OIDC discovery，IdP 不可达会直接退出。离线环境需要内网 Keycloak/Dex：

- IdP 镜像不在 KubeDeck 离线包内，需单独准备；
- 必须在 IdP 中配置 `groups` claim 映射（用于 Impersonate-Group）；
- 若 IdP 不返回 `email_verified`，只能用 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true` 作为受控例外；
- `dev.enabled` 跳过认证与权限校验，禁止用于生产。

## 体积预算（实测基准）

| 镜像 | 未压缩 | gzip 后 |
|---|---|---|
| `kubedeck:0.1.0` | 68.2 MB | 16.4 MB |
| `postgres:17-alpine` | 424 MB | 116 MB |
| `caddy:2-alpine` | 92 MB | 25 MB |

体积主要来自 Go 二进制（client-go 占大头，约 40 MB）和 distroless 基础层（约 22 MB）；前端 `go:embed` 后约 7 MB。应用镜像压缩率约 76%，已经很小，继续优化（UPX、裁剪 discovery）收益有限且影响可维护性，不在本次范围。

## 风险与取舍

- **镜像 tag 可变**：脚本默认使用源码版本 `0.1.0`，同名 tag 重复构建会覆盖；发布时应使用不可变版本号，并在 `manifest.txt` 记录 Git commit 与构建时间。
- **`docker save` 包含全部层**：无法只导增量层；升级包仍接近全量。对 35 MB 级应用镜像可接受。
- **内部 CA 信任成本**：`tls internal` 省去证书申请，但每台客户端都要安装根证书；生产更推荐企业 CA 证书。
- **不内置 IdP**：把 Keycloak 塞进离线包会显著扩大交付面并引入额外数据库与升级责任，明确排除。