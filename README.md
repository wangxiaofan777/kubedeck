# KubeDeck · 集群驾驶舱

> 一个把 Kubernetes 日常操作收敛到统一入口的 Web 管理控制台：有权限边界、有实时状态、有完整审计。

**Deck** 是舰船的驾驶舱/甲板——所有仪表、航向与操作都集中在这一层。KubeDeck 不是再造一个 kubectl，而是给多集群一个统一的操控台。中文名取「集群驾驶舱」，内部沟通时可直接叫 **Deck**。

## 当前状态

截至 2026-11，已完成「可运行骨架 + 数据层 + 只读控制台 + 工程基线」：

| 已完成 | 说明 |
|---|---|
| 服务端骨架 | chi 路由、统一错误模型、请求中间件、优雅退出、`/healthz` 与 `/readyz` |
| 数据层 | PostgreSQL 迁移、审计月分区与哈希链、AES-256-GCM 凭据加密、用户/集群/绑定仓储 |
| 只读控制台 | 集群与命名空间切换、13 类资源列表、详情抽屉、实时 YAML |
| 工程基线 | Makefile、staticcheck、ESLint/Prettier、CI（含 PostgreSQL 集成测试） |

尚未实现（按 `openspec/changes/add-kubedeck-mvp/tasks.md`）：

- OIDC 登录、会话、Impersonation 透传、权限预检（3.x）
- 集群登记写接口与健康探测（4.x）
- YAML 提交、伸缩/重启/回滚/删除（5.x–6.x）
- 日志与事件（7.x）、实时推送（8.x）、审计埋点（9.x）、完整控制台（10.x）

> **安全提示**：当前开发模式用本地 kubeconfig 直连集群，**没有登录与权限校验**，只能在本机运行，禁止暴露到网络。

## 快速开始

前置依赖：

| 项 | 版本 | 用途 |
|---|---|---|
| Go | 1.27 | 后端构建与测试 |
| Node.js | 24 | 前端构建（Windows 侧或 WSL 内均可） |
| PostgreSQL | 15+ | 平台元数据与审计 |
| k3s | 1.34–1.37 | 本地测试集群（可选，但看资源列表需要） |

```bash
# 1) 初始化本地数据库（幂等；连接串写入 .testenv/db.env）
wsl.exe -d Ubuntu -u root -- bash -lc 'cd /mnt/d/AI_Coding/kubedeck && bash scripts/dev-db.sh'

# 2) 构建前端（Windows 侧）
cd web && npm ci --no-audit --no-fund && npm run build

# 3) 构建后端（WSL 内；make 会自动找到 /usr/local/go/bin/go）
wsl.exe -d Ubuntu -u root -- bash -lc 'cd /mnt/d/AI_Coding/kubedeck && make server'

# 4) 启动（WSL 内；需要 k3s 与 .testenv/k3s.yaml）
wsl.exe -d Ubuntu -u root -- bash -lc '
  cd /mnt/d/AI_Coding/kubedeck &&
  set -a && . ./.testenv/db.env && set +a &&
  KUBEDECK_ADDR=0.0.0.0:8080 \
  KUBEDECK_DEV_KUBECONFIG=/mnt/d/AI_Coding/kubedeck/.testenv/k3s.yaml \
  ./bin/kubedeck
'
```

浏览器打开 <http://localhost:8080>。完整的 k3s 安装、镜像加速与故障排查见 [docs/local-environment.md](docs/local-environment.md)。

## 目录结构

```
.
├── cmd/server/                  # 服务端入口：配置、日志、信号，其余交给 internal/app
├── internal/
│   ├── app/                     # 依赖组装与生命周期（HTTP + 后台任务 + 数据库）
│   ├── api/                     # 路由、handler、错误模型、中间件、静态资源
│   ├── config/                  # 环境变量与 YAML 配置，启动前校验
│   ├── crypto/                  # AES-256-GCM 凭据加密与密钥版本
│   ├── domain/                  # 领域模型与领域错误
│   ├── kube/                    # ClusterTransport、Direct 实现、集群注册表
│   └── store/                   # PostgreSQL 连接、迁移、仓储
├── web/
│   ├── src/app/                 # Providers 与控制台外壳
│   ├── src/features/            # clusters / namespaces / resources
│   ├── src/components/          # 资源表格、详情抽屉、错误告警
│   └── src/lib/                 # API 客户端与格式化工具
├── internal/webui/dist/         # Vite 构建产物（仅 .gitkeep 入库，供 go:embed）
├── scripts/                     # 本地开发脚本（数据库初始化）
├── verify/                      # 技术验证程序（独立 Go module）
├── docs/                        # 环境手册与文档索引
└── openspec/                    # 规格、设计与实施任务
```

## 常用命令

| 命令 | 作用 |
|---|---|
| `make build` | 构建前端 + 后端单二进制（需要 npm 可用） |
| `make server` | 只构建后端；依赖 `internal/webui/dist` 占位，可用于后端联调 |
| `make web-dev` | 启动 Vite dev server（`/api` 代理到 8080） |
| `make test` | 后端单元测试（数据层用例在无数据库时跳过） |
| `make test-db` | 数据层集成测试（连接 `.testenv/db.env` 中的 PostgreSQL） |
| `make lint` | gofmt 检查 + `go vet` + `staticcheck` |
| `make check` | 后端 lint/测试 + 前端 lint/typecheck（提交前入口） |
| `make clean` | 清理构建产物并保留 `internal/webui/dist/.gitkeep` |
| `make db` | 创建/重置本地数据库角色与库 |

前端单独命令：`npm run lint`、`npm run typecheck`、`npm run format`、`npm run build`。

## 技术栈

- **后端**：Go 1.27、client-go v0.37.2（dynamic client + discovery）、chi、pgx v5
- **前端**：React 18、TypeScript、Ant Design 5、TanStack Query 5、Vite 5
- **存储**：PostgreSQL 15+（平台元数据与审计；集群资源状态不入库）
- **授权**：Impersonation 透传 + Kubernetes 原生 RBAC（后续任务）
- **部署**：单二进制（前端 `go:embed`）+ Docker/Helm（Helm 待实现）

## 测试与质量

- 后端：`make test`；数据层集成测试需 PostgreSQL。
- 前端：ESLint + Prettier + `tsc --noEmit`。
- CI：后端（gofmt/vet/staticcheck/`go test -race` + PostgreSQL service）、前端（lint/format/typecheck/build）、镜像构建。
- 版本兼容矩阵：k3s 1.34 与 1.37 的实测结果见 [docs/local-environment.md §7.1](docs/local-environment.md)。

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

## 文档

| 文档 | 内容 |
|---|---|
| [k8s-console-design.md](k8s-console-design.md) | 产品级设计总纲（架构、API、错误码、容量估算） |
| [docs/local-environment.md](docs/local-environment.md) | WSL + k3s 环境、数据库、多版本矩阵 |
| [docs/README.md](docs/README.md) | 文档索引与阅读顺序 |
| [AGENTS.md](AGENTS.md) | 给 AI 助手与新同学的项目约定 |
| [verify/README.md](verify/README.md) | dynamic client / Impersonation / exec 三项验证结论 |