# docs

面向人的说明文档。与 `openspec/` 的区别：**openspec 是给开发流程用的可执行规格，docs 是给人读的背景与操作手册**。

| 文档 | 内容 | 什么时候看 |
|---|---|---|
| [local-environment.md](local-environment.md) | WSL + k3s 环境搭建、数据库、镜像加速、常见问题、多版本矩阵 | 第一次搭环境、环境坏了、换机器 |
| [../k8s-console-design.md](../k8s-console-design.md) | 产品设计总纲：架构、模块、API、安全、路线图、附录 | 想了解整体设计、评审方案 |
| [../verify/README.md](../verify/README.md) | 三项技术验证的结果、命令与判读标准 | 确认能力可行、复现验证 |
| [../AGENTS.md](../AGENTS.md) | 给 AI 助手的项目约定与禁止事项 | 新开窗口、换助手 |
| [../README.md](../README.md) | 项目定位、当前状态、快速开始、工程命令 | 第一次接触项目 |

## 工程命令

| 命令 | 作用 |
|---|---|
| `make build` | 前端 + 后端单二进制构建 |
| `make server` | 只构建后端（依赖 `internal/webui/dist` 占位） |
| `make check` | 提交前入口：后端 lint/测试 + 前端 lint/typecheck |
| `make test-db` | 数据层集成测试（真实 PostgreSQL） |
| `make db` | 初始化本地数据库角色与库 |

更完整的说明见根 [README.md](../README.md) 与 [local-environment.md](local-environment.md)。

## 阅读顺序建议

1. `README.md` — 5 分钟了解这是什么
2. `k8s-console-design.md` §1–§4 — 定位、选型、架构、核心模块
3. `openspec/changes/add-kubedeck-mvp/proposal.md` — 本次变更的范围
4. `openspec/changes/add-kubedeck-mvp/design.md` — 技术决策与权衡
5. `openspec/changes/add-kubedeck-mvp/specs/` — 验收标准
6. `openspec/changes/refactor-engineering-baseline/` — 工程基线的提案、设计与任务
7. `docs/local-environment.md` — 动手前

## 文档维护约定

- 设计决策变化 → 先改 `openspec/.../design.md`，再同步 `k8s-console-design.md`
- 行为变化 → 先改 spec，再改实现
- 环境步骤变化 → 只改 `docs/local-environment.md`，其他文档引用它