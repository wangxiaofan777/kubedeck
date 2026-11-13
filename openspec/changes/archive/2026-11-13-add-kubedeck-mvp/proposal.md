## Why

团队当前管理 Kubernetes 的方式是"发 kubeconfig + 教 kubectl"：凭据在多人之间扩散、权限边界依赖集群侧配置、误操作（删错命名空间、改错镜像）无法追溯，新人上手成本高。随着集群数量从 1 个增长到多个，需要一个统一的、有权限边界、全量审计的 Web 入口，把高频操作收敛起来，同时不放弃 K8S 原生 RBAC 这一事实来源。

## What Changes

- 新建项目 **KubeDeck**（中文名：集群驾驶舱），B/S 形态的多集群管理控制台，后端 Go + client-go，前端 React + TypeScript。
- 新增 8 项平台能力：集群接入、身份与授权、资源浏览、工作负载操作、日志与事件、实时推送、审计、Web 控制台。
- 授权采用 **Impersonation 透传**：平台不复制 K8S 权限规则，由 apiserver 按被模拟用户身份做原生 RBAC 判定；平台侧只维护"用户 × 集群 × 命名空间 × 角色"的准入范围。
- 集群资源状态**不落库**，实时读取 apiserver 与 informer 缓存；PostgreSQL 只保存平台元数据与审计。
- 第一阶段仅支持直连集群（Direct），代码预留 `ClusterTransport` 抽象以备后续 Agent 反向隧道。
- 明确不做：替代 kubectl 的全量能力、CI/CD 流水线、服务网格控制面、集群生命周期管理、通用监控平台。

## Capabilities

### New Capabilities

- `cluster-registry`：集群接入、kubeconfig 凭据加密存储与轮换、连通性健康检查、集群级访问控制入口。
- `identity-access`：OIDC 登录与会话管理、用户身份映射、Impersonation 透传、`SelfSubjectAccessReview` 权限预检、角色绑定管理。
- `resource-browser`：API 资源发现（含 CRD）、资源列表与详情、YAML 查看/编辑/Diff/提交、删除与批量操作、资源关联关系。
- `workload-control`：Deployment/StatefulSet/DaemonSet/Job/CronJob/Pod 的专属操作（伸缩、重启、回滚、暂停、驱逐）。
- `log-and-events`：Pod 日志查看与流式跟随、多容器切换、事件时间线与过滤。
- `realtime-stream`：单连接多路复用 WebSocket，列表页增量更新、断线重连、慢消费者降级。
- `audit-log`：全量写操作与 exec/凭据操作的不可变审计、查询、导出与防篡改校验。
- `web-console`：前端信息架构、路由、权限感知 UI、危险操作确认、列表/详情/编辑的通用交互约定。

### Modified Capabilities

（无，本项目为绿地初始化）

## Impact

- **新增代码库结构**：`cmd/server`、`internal/{api,domain,kube,store,auth,crypto}`、`web/`、`charts/kubedeck`。
- **新增外部依赖**：PostgreSQL 15+（必需）、Redis 7+（推荐）、Ingress + TLS（必需）、OIDC IdP（必需）。
- **目标集群权限**：平台 ServiceAccount 需要 `users`/`groups`/`serviceaccounts` 的 `impersonate` 权限，以及资源发现权限；不得授予 `cluster-admin`。
- **安全边界**：平台凭据加密依赖主密钥（KMS/Vault/环境变量），需在部署文档中作为强制项。
- **测试环境**：基于 WSL2 + k3s 的可复现环境，验证脚本位于 `verify/`；不依赖开发者本地已有集群。
- **与现有资产的关系**：`k8s-console-design.md` 为产品级设计总纲，本变更的 spec 为其可执行、可验收的切片。