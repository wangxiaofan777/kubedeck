# KubeDeck 项目约定

> 给 AI 编码助手与新加入的同学：先读这一页，再动手。

## 这是什么

KubeDeck（集群驾驶舱）是一个多集群 Kubernetes Web 管理控制台。**当前已完成服务端骨架、数据层、只读控制台与工程基线；认证、写操作、实时推送尚未实现**（进度以 `openspec/changes/add-kubedeck-mvp/tasks.md` 为准）。

## 先读什么（按顺序）

1. `README.md` — 项目定位、目录、技术栈
2. `openspec/changes/add-kubedeck-mvp/proposal.md` — 为什么做、做什么、不做什么
3. `openspec/changes/add-kubedeck-mvp/design.md` — 技术决策、API 契约、数据映射、关键时序、性能预算
4. `openspec/changes/add-kubedeck-mvp/specs/` — 8 个能力的验收标准
5. `k8s-console-design.md` — 产品级设计总纲（含 API 清单、错误码、WS 协议、容量估算等附录；以 spec 为验收依据）
6. `docs/local-environment.md` — 动手前先看环境手册

## 工作方式：规格先行

本项目使用 OpenSpec（`openspec` CLI 已安装）做规格驱动开发。

- **所有改动都要先有提案**：在 `openspec/changes/<change>/` 下创建 proposal/design/tasks；行为变化还要写 spec，纯重构/工具链变更也要有 proposal 与 tasks（无行为变化时在 `.openspec.yaml` 声明 `skip_specs: true`）
- 任何**行为变化**，先改 `openspec/changes/<change>/specs/` 下的规格，再改代码
- 规格用 `### Requirement:` + `#### Scenario:`（四个 `#`），需求用 SHALL/MUST
- 写完执行 `openspec validate <change> --strict`，必须通过
- 实施进度在 `tasks.md` 里勾选 `- [ ]`，不要另建 TODO 文件
- 规格描述"做什么"，实现细节写 `design.md`，不要把类名/框架选择写进 spec

常用命令：

```bash
openspec list
openspec show add-kubedeck-mvp
openspec status --change add-kubedeck-mvp
openspec validate add-kubedeck-mvp --strict
```

## 不可违背的架构约束

1. **授权只走 Impersonation**：平台 ServiceAccount 仅授予 `users`/`groups`/`serviceaccounts` 的 `impersonate` 动词，**禁止**授予 `cluster-admin`；权限判定由 apiserver 完成，平台不维护第二套权限表。
2. **集群资源状态不入库**：PostgreSQL 只存平台元数据与审计；列表读 informer 缓存，详情按需 GET。
3. **资源发现必须容忍部分失败**：`ServerPreferredResources()` 出错时用 `discovery.IsGroupDiscoveryFailedError` 判断并继续，不能让一个聚合 API 拖垮整个资源目录。
4. **写操作必须审计**：审计异步写入、只追加、带哈希链；请求体只存摘要哈希。
5. **前端隐藏不等于授权**：所有写请求在服务端重新校验。
6. **不在本期引入 Agent 隧道**：只保留 `ClusterTransport` 抽象，Direct 是唯一实现。

## 技术栈与版本

| 层 | 选型 | 版本 |
|---|---|---|
| 后端 | Go + client-go | Go 1.27 / client-go v0.37.2 |
| 前端 | React + TypeScript + Ant Design 5 | React 18 |
| 数据库 | PostgreSQL | 15+ |
| 缓存 | Redis（可选） | 7+ |
| 测试集群 | k3s | v1.37.2 |

## 本地测试环境

- 集群跑在 **WSL2 Ubuntu** 里，由 systemd 管理：
  ```bash
  wsl.exe -d Ubuntu -u root -- systemctl start k3s
  wsl.exe -d Ubuntu -u root -- k3s kubectl get nodes
  ```
- kubeconfig：`.testenv/k3s.yaml`（指向 `127.0.0.1:6443`）
- **Windows 到 WSL 的 localhost 转发不稳定**，联调与测试命令尽量在 WSL 内执行
- Go 路径：Windows `C:\Users\maosong.wang\tools\go`，WSL `/usr/local/go`
- 模块代理：`GOPROXY=https://goproxy.cn,direct`
- 镜像加速已配置在 WSL 的 `/etc/rancher/k3s/registries.yaml`（docker.m.daocloud.io）
- 环境搭建、验证命令与故障排查见 `docs/local-environment.md`（唯一权威来源）

## 测试

```bash
# 后端单元测试
make test

# 数据层集成测试（连接 .testenv/db.env 中的 PostgreSQL，见 docs/local-environment.md §7.3）
make test-db

# 提交前统一检查：后端 lint/测试 + 前端 lint/typecheck
make check

# 三项基础能力验证（完整命令见 docs/local-environment.md §7）
wsl.exe -d Ubuntu -u root -- bash -lc "cd /root/kubedeck-verify && ./verify -mode=list -kubeconfig /etc/rancher/k3s/k3s.yaml -ns kube-system"
```

## 不要做

- 不要把 kubeconfig、token、证书私钥写进文档、日志或提交记录
- 不要绕过 OpenSpec 直接改行为；不要为了让校验通过而编造需求
- 不要把集群资源状态同步进 PostgreSQL
- 不要在 spec 里写实现细节（类名、库选型、步骤）
- 不要在没有说明的情况下扩大范围（CI/CD、服务网格、集群生命周期都不是本项目）