# KubeDeck · 集群驾驶舱

> 一个把 Kubernetes 日常操作收敛到统一入口的 Web 管理控制台：有权限边界、有实时状态、有完整审计。

**Deck** 是舰船的驾驶舱/甲板——所有仪表、航向与操作都集中在这一层。KubeDeck 不是再造一个 kubectl，而是给多集群一个统一的操控台。中文名取「集群驾驶舱」，内部沟通时可直接叫 **Deck**。

## 当前状态

MVP 功能已完成并通过本地 k3s 端到端验收（2026-11-12）：

| 能力 | 状态 |
|---|---|
| 身份与授权 | OIDC + PKCE、服务端会话、Impersonation、SSAR 预检、首管理员引导 |
| 多集群接入 | 登记/轮换/删除、凭据 AES-256-GCM、健康探测、1.18–1.37 版本校验（1.18 为 legacy 兼容档） |
| 资源浏览 | discovery（容忍部分失败）、分页/过滤/排序、详情/事件、YAML 编辑与 Diff |
| 工作负载 | 伸缩、重启、历史/回滚、暂停/恢复、Pod 驱逐、操作进度 |
| 日志与实时 | 多容器日志、跟随/暂停/过滤；WebSocket 多路复用、续传/resync |
| 审计 | 异步写入、哈希链校验、查询与 CSV/JSON 导出 |
| 控制台 | 深链接、权限组件、虚拟滚动、全局搜索、空/错/降级态 |
| 部署 | Docker Compose 单机部署、最小权限 SA、Prometheus 指标、运维/备份/发布文档 |

完整任务进度见 [`openspec/changes/archive/2026-11-13-add-kubedeck-mvp/tasks.md`](openspec/changes/archive/2026-11-13-add-kubedeck-mvp/tasks.md)。

> **安全提示**：dev 模式用本地 kubeconfig 直连集群，使用本地开发身份，**没有企业 IdP 与集群权限校验**。首次使用需在登录页显式进入，只能在本机运行，禁止暴露到网络。生产请使用 OIDC + HTTPS。

## 快速开始

前置依赖：Go 1.27、Node.js 24、PostgreSQL 15+ 或 MySQL 8.0.20+（默认 PostgreSQL）、k3s 1.18–1.37（可选，但看资源列表需要；1.18 为 legacy 兼容档）。

```bash
# 1) 初始化本地数据库（幂等；连接串写入 .testenv/db.env）
wsl.exe -d Ubuntu -u root -- bash -lc 'cd /mnt/d/AI_Coding/kubedeck && bash scripts/dev-db.sh'

# 2) 构建前端与后端
cd web && npm ci --no-audit --no-fund && npm run build && cd ..
wsl.exe -d Ubuntu -u root -- bash -lc 'cd /mnt/d/AI_Coding/kubedeck && make server'

# 3) 启动（WSL 内；需要 k3s 与 .testenv/k3s.yaml）
wsl.exe -d Ubuntu -u root -- bash -lc '
  cd /mnt/d/AI_Coding/kubedeck &&
  set -a && . ./.testenv/db.env && set +a &&
  KUBEDECK_ADDR=0.0.0.0:8080 \
  KUBEDECK_DEV_KUBECONFIG=/mnt/d/AI_Coding/kubedeck/.testenv/k3s.yaml \
  ./bin/kubedeck
'
```

浏览器打开 <http://localhost:8080>。生产部署见 [docs/deployment.md](docs/deployment.md)，本地环境见 [docs/local-environment.md](docs/local-environment.md)。

### Docker Compose（单机）

唯一的部署方式：

```bash
cd deploy/compose
cp .env.example .env && chmod 600 .env
# 填写数据库密码、主密钥与 OIDC 配置
docker compose up -d --build
```

应用固定单实例，前面需要一个终止 HTTPS 的反向代理。完整步骤（OIDC、管理员引导、集群接入、备份、升级、排障）见 [docs/deployment.md](docs/deployment.md)。

MySQL 8 仍受后端支持：把 `KUBEDECK_DATABASE_URL` 指向外部 MySQL 并设置 `KUBEDECK_DB_DRIVER=mysql`，做法见部署手册 §7.3。

## 目录结构

```
.
├── cmd/server/                  # 服务端入口
├── internal/
│   ├── app/                     # 依赖组装与生命周期
│   ├── api/                     # 路由、handler、中间件、WebSocket、审计埋点
│   ├── audit/                   # 异步审计写入与补偿
│   ├── auth/                    # OIDC、会话、Impersonation 身份
│   ├── cluster/                 # 集群登记、凭据、健康探测
│   ├── crypto/                  # AES-256-GCM 与密钥版本
│   ├── kube/                    # ClusterTransport、Direct 实现、注册表
│   ├── metrics/                 # Prometheus 指标
│   └── store/                   # 仓储契约与 PostgreSQL/MySQL 实现
├── web/                         # React 控制台
├── deploy/compose/              # Docker Compose 单机部署（应用 + PostgreSQL）
├── docs/                        # 部署、运维、备份、安全、发布文档
├── verify/                      # 三项技术验证程序
└── openspec/                    # 规格、设计与任务
```

## 常用命令

| 命令 | 作用 |
|---|---|
| `make build` | 构建前端 + 后端单二进制 |
| `make server` | 只构建后端（依赖 dist 占位） |
| `make web-dev` | Vite dev server（`/api` 代理到 8080） |
| `make test` | 后端单元测试 |
| `make test-db` | 数据层集成测试（真实 PostgreSQL） |
| `make test-db-mysql` | 数据层集成测试（真实 MySQL 8） |
| `make lint` | gofmt + `go vet` + `staticcheck` |
| `make check` | 后端 lint/测试 + 前端 lint/typecheck |
| `make clean` | 清理产物并保留 `internal/webui/dist/.gitkeep` |

前端单独命令：`npm run lint`、`npm run typecheck`、`npm run format`、`npm run build`。

## 技术栈

- **后端**：Go 1.27、client-go v0.37.2（dynamic client + discovery + watch）、chi、pgx v5、go-sql-driver/mysql
- **前端**：React 18、TypeScript、Ant Design 5、TanStack Query 5、Monaco、Vite 5
- **存储**：PostgreSQL 15+（默认）或 MySQL 8.0.20+，通过 `KUBEDECK_DATABASE_URL` + `KUBEDECK_DB_DRIVER` 选择；集群资源状态不入库
- **授权**：Impersonation 透传 + Kubernetes 原生 RBAC
- **部署**：单二进制（前端 `go:embed`）+ Docker + Docker Compose
- **可观测性**：Prometheus `/metrics`、结构化日志、哈希链审计

## 测试与质量

- 后端：`make test`；数据层集成测试需 PostgreSQL 或 MySQL 8（`make test-db` / `make test-db-mysql`）；列表压测见 `internal/api/load_test.go`。
- 前端：ESLint + Prettier + `tsc --noEmit` + Vite build。
- CI：后端 lint/vet/staticcheck/`go test -race` + 真实 PostgreSQL 与 MySQL；前端 lint/typecheck/build；镜像构建。
- 版本矩阵：k3s 1.34 与 1.37 实测见 [docs/local-environment.md §7.1](docs/local-environment.md)；1.18.16 legacy 兼容测试见 §7.4。

## OpenSpec

本项目使用 [OpenSpec](https://github.com/Fission-AI/OpenSpec) 做规格驱动开发：行为变化先改规格，再改代码。

```bash
openspec list
openspec show add-kubedeck-mvp
openspec validate add-kubedeck-mvp --strict
openspec view
```

| 变更 | 内容 |
|---|---|
| `add-kubedeck-mvp` | MVP 8 项能力：集群接入、身份授权、资源浏览、工作负载、日志、实时、审计、控制台 |
| `refactor-engineering-baseline` | 工程基线：可复现构建、质量闸门、配置校验、就绪探测、安全头与保留期保护 |
| `add-mysql-metadata-store` | 元数据存储可插拔：新增 MySQL 8 实现、双引擎一致性测试与部署路径 |

## 文档

| 文档 | 内容 |
|---|---|
| [k8s-console-design.md](k8s-console-design.md) | 产品级设计总纲（架构、API、错误码、容量估算） |
| [docs/deployment.md](docs/deployment.md) | 部署手册：Compose、OIDC、最小权限 SA、数据库与升级 |
| [docs/operations.md](docs/operations.md) | 升级/回滚/故障处理/容量 |
| [docs/backup-restore.md](docs/backup-restore.md) | PostgreSQL / MySQL 备份、恢复与演练 |
| [docs/permission-matrix.md](docs/permission-matrix.md) | 平台角色 × 操作 × Kubernetes 权限 |
| [docs/security-review.md](docs/security-review.md) | 安全自查结论与残余风险 |
| [docs/release-notes.md](docs/release-notes.md) | 版本说明与兼容矩阵 |
| [docs/release-checklist.md](docs/release-checklist.md) | 发布检查单 |
| [docs/openapi.yaml](docs/openapi.yaml) | OpenAPI 3 文档（测试保证与路由一致） |
| [docs/local-environment.md](docs/local-environment.md) | WSL + k3s 环境、数据库、多版本矩阵 |
| [docs/README.md](docs/README.md) | 文档索引与阅读顺序 |
| [AGENTS.md](AGENTS.md) | 给 AI 助手与新同学的项目约定 |
| [verify/README.md](verify/README.md) | dynamic client / Impersonation / exec 三项验证结论 |