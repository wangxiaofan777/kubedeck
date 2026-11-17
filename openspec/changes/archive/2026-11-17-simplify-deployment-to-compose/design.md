## 目标

让“部署 KubeDeck”只有一种读法：一个 Dockerfile、一个 Compose 文件、一份文档。删除长期未验证的并行路径，降低维护面，同时不削弱应用能力。

## 关键决策

### 1. 只保留 Docker Compose 单机部署

Helm Chart 包含 Deployment/Service/Ingress/PDB/HPA/NetworkPolicy/ServiceMonitor 等 13 个模板，需要 Kubernetes、Ingress 控制器、可能的 Prometheus Operator 才能完整验证；MVP 的验收与日常使用都走 Compose。删除 Chart 后：

- 部署方只需要 Docker Engine + Compose v2；
- 不再需要维护 `--kube-version 1.18` 的条件渲染分支；
- 平台对**目标集群** 1.18–1.37 的兼容能力不受影响，那是控制台访问的集群，不是 KubeDeck 自身部署环境。

代价：失去 Kubernetes 原生部署、滚动升级与 PDB/HPA。当前版本会话在进程内、后台任务无选主，本来也不支持多副本，Helm 的高可用收益并不存在；需要时再以独立变更引入。

### 2. Compose 只保留一种数据库组合，TLS 交给前置代理

- 应用 + PostgreSQL 是默认且唯一编排；MySQL 仍受后端支持，部署方改 `KUBEDECK_DATABASE_URL` 并设置 `KUBEDECK_DB_DRIVER=mysql` 指向外部 MySQL。
- 删除 Caddy：证书申请、域名与 80/443 绑定属于部署环境职责。Compose 只映射应用端口，文档要求前面必须有一个终止 HTTPS 的反向代理，并保留 WebSocket 所需的 `flush` 配置说明。
- 删除 `.env.mysql.example` 与离线覆盖文件，避免“每个变体一套文件”。

### 3. 删除离线打包脚本，保留最小手工路径

`offline-bundle.sh` 约 250 行，负责 buildx、导出、gzip、校验和、manifest、依赖镜像、内网 registry 推送脚本。维护成本高，且产物与 Compose 变体强耦合。删除后，内网交付按通用做法执行：

```bash
docker build -t kubedeck:<version> .
docker save kubedeck:<version> | gzip > kubedeck-<version>.tar.gz
# 目标机
gunzip -c kubedeck-<version>.tar.gz | docker load
```

文档只保留这三条命令与“镜像必须提前导入”的提醒，不再维护打包工具。

### 4. 删除 Prometheus 告警规则，保留 `/metrics`

应用 `/metrics` 端点、指标名称与运维文档中的抓取说明保持不变；只删除 `deploy/prometheus/` 的 `PrometheusRule` 与对应的 `make test-rules`。告警规则与具体监控栈（Prometheus Operator / 普通 Prometheus / 云监控）强绑定，放在仓库里既无法在 CI 验证，也容易过期。

### 5. 文档合并，历史记录不回改

- `docs/deployment.md` 变成唯一部署文档，吸收原 Compose README 中仍有价值的部分：启动、初始化管理员、接入集群、网络、单副本限制、备份、升级回滚、排障。
- 删除 `docs/offline-deployment.md`、`docs/alerting.md`、`deploy/compose/README.md`。
- `docs/acceptance.md`、`docs/release-notes.md`、`docs/security-review.md` 是历史记录，保留原文，不回改成“从未用过 Helm”的叙述。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 使用 Helm 的现有部署无法直接升级 | 在 proposal 与文档中给出迁移说明：镜像与 schema 不变，改用 Compose 或自行维护清单 |
| 删除 Caddy 后部署方忘记配置 HTTPS | 部署文档把“前置 HTTPS 反向代理”列为前置条件，并说明 Secure Cookie 在纯 HTTP 下不工作 |
| 内网交付失去一键打包 | 文档给出 `docker save/load` 三步；离线只是传输方式，不再是独立编排变体 |
| 后续又长出并行部署文件 | 在 `engineering-baseline` 规格中新增“部署制品收敛”，验收时检查 `deploy/` 只含单文件 Compose |