# KubeDeck · 集群驾驶舱

> 一个把 Kubernetes 日常操作收敛到统一入口的 Web 管理控制台：有权限边界、有实时状态、有完整审计。

```
        ┌─────────────────────────────────────┐
        │  KubeDeck                           │
        │  cluster · namespace · resource     │
        └─────────────────────────────────────┘
```

## 为什么叫 KubeDeck

**Deck** 是舰船的驾驶舱/甲板——所有仪表、航向与操作都集中在这一层。KubeDeck 想做的正是这件事：不是再造一个 kubectl，而是给多集群一个统一的操控台。中文名取「集群驾驶舱」，内部沟通时可直接叫 **Deck**。

命名备选（未采用）：KubeScope（偏观测）、KubeBoard（偏看板）。

## 当前状态

**设计阶段已完成，尚未开始编码。**

| 产出 | 位置 | 说明 |
|---|---|---|
| 产品设计总纲 | [k8s-console-design.md](k8s-console-design.md) | 架构、模块、API、安全、路线图，含 API 清单/错误码/WS 协议/容量估算等 6 个附录 |
| 技术验证 | [verify/README.md](verify/README.md) | dynamic client / Impersonation / exec 三项已跑通 |
| 可执行规格 | [openspec/](openspec/) | 8 个能力、53 条需求、103 个场景、77 个实施任务 |
| 环境手册 | [docs/local-environment.md](docs/local-environment.md) | WSL + k3s 搭建、镜像加速、常见问题 |
| 验证代码 | [verify/main.go](verify/main.go) | 单文件 250 行，四个子命令 |

## 技术栈

- **后端**：Go 1.27 + client-go v0.37（dynamic client + informer）
- **前端**：React 18 + TypeScript + Ant Design 5 + TanStack Query + Monaco
- **存储**：PostgreSQL（平台元数据与审计）+ Redis（会话，可选）
- **授权**：Impersonation 透传 + K8S 原生 RBAC，平台不维护第二套权限规则
- **部署**：单二进制（内嵌前端）+ Helm

## OpenSpec

本项目使用 [OpenSpec](https://github.com/Fission-AI/OpenSpec) 做规格驱动开发。所有需求以 `Requirement` / `Scenario` 形式表达，开发前先过规格，完成后由 `openspec archive` 归档为长期规格。

```bash
openspec list                    # 查看进行中的变更
openspec show add-kubedeck-mvp   # 查看变更详情
openspec validate --strict       # 校验规格完整性
openspec view                    # 交互式面板
```

当前变更：`add-kubedeck-mvp`，包含 8 个能力：

| 能力 | 覆盖内容 |
|---|---|
| `cluster-registry` | 集群接入、凭据加密、健康检查、可见范围 |
| `identity-access` | OIDC 登录、Impersonation、权限预检、角色绑定 |
| `resource-browser` | 资源发现、列表详情、YAML 编辑、删除 |
| `workload-control` | 伸缩、重启、回滚、暂停、Pod 处理 |
| `log-and-events` | 日志查看与跟随、事件聚合 |
| `realtime-stream` | 单连接多路复用、增量推送、断线续传 |
| `audit-log` | 记录范围、不可变性、查询导出 |
| `web-console` | 导航、权限感知、危险操作确认 |

## 目录结构

```
.
├── k8s-console-design.md          # 产品设计总纲（含 6 个附录）
├── docs/
│   ├── README.md                  # 文档索引
│   └── local-environment.md       # 环境搭建手册
├── openspec/
│   ├── config.yaml                # OpenSpec 配置（zh-CN）
│   ├── changes/add-kubedeck-mvp/  # 本次变更：proposal / specs / design / tasks
│   └── specs/                     # 归档后的长期规格（当前为空）
├── verify/                        # 技术验证程序与报告
└── .testenv/k3s.yaml              # 本地 k3s 测试集群配置（不入库）
```

## 下一步

按 `openspec/changes/add-kubedeck-mvp/tasks.md` 从 1.1 开始实施；建议先在 k3s 测试集群完成 Phase 0 的端到端链路（登录 → 接集群 → 浏览资源），再进入写操作。