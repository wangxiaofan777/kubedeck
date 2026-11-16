## 1. 提案与脚本骨架

- [x] 1.1 创建 `openspec/changes/add-offline-deployment`（proposal/design/tasks，`skip_specs: true`）
- [x] 1.2 新增 `scripts/offline-bundle.sh`：参数解析、前置检查、版本/平台/输出目录
- [x] 1.3 实现镜像构建、导出、gzip 压缩与 `SHA256SUMS`
- [x] 1.4 生成 `manifest.txt` 与交付物清单，支持 `--no-deps`、`--registry`

## 2. Compose 离线编排

- [x] 2.1 新增 `deploy/compose/docker-compose.offline.yml`：`pull_policy: never`，禁止现场构建
- [x] 2.2 新增 `deploy/compose/Caddyfile.offline`：`tls internal`，并保留自有证书替换说明
- [x] 2.3 更新 `.env.example` 与 Compose README，说明离线变量与根证书导出
- [x] 2.4 Helm Chart 增加 `extraVolumes` / `extraVolumeMounts`，支持挂载内网 CA

## 3. 文档

- [x] 3.1 新增 `docs/offline-deployment.md`：体积、构建、导出、传输校验、导入、Compose/Helm、TLS、OIDC、升级回滚、验收
- [x] 3.2 根 `README.md`、`docs/README.md`、`docs/deployment.md` 增加入口
- [x] 3.3 记录实测镜像体积与压缩比

## 4. 验收

- [x] 4.1 `bash -n scripts/offline-bundle.sh` 通过
- [x] 4.2 在联网机构建 `linux/amd64` 镜像并核对 `docker images` 体积（68.2 MB）
- [x] 4.3 导出后用 `sha256sum -c` 校验，`docker load` 可正常导入
- [x] 4.4 `docker compose -f docker-compose.yml -f docker-compose.offline.yml config` 渲染通过
- [x] 4.5 `helm lint` / `helm template` 使用本地镜像名与额外卷渲染通过
- [x] 4.6 `openspec validate add-offline-deployment --strict` 通过