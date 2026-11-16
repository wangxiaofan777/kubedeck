## 1. 后端

- [x] 1.1 `internal/kube` 实现 Cordon / Uncordon（merge patch）
- [x] 1.2 实现 Drain：列 Pod、过滤、逐 Pod Eviction、逐项结果
- [x] 1.3 新增节点路由：cordon / uncordon / drain / pods
- [x] 1.4 drain 使用独立 120 秒 context，支持请求体覆盖上限
- [x] 1.5 审计动词映射 node.cordon / node.uncordon / node.drain
- [x] 1.6 补单元测试：过滤规则、PDB 失败、部分成功、超时

## 2. 前端

- [x] 2.1 节点列表页（角色、状态、版本、可分配资源、Pod 数、可调度）
- [x] 2.2 节点详情页（Conditions、容量、Taints、Labels、Pod 列表）
- [x] 2.3 cordon/uncordon 操作与反馈
- [x] 2.4 drain Drawer：影响清单、选项、进度、逐项结果
- [x] 2.5 metrics 不可用时的降级展示

## 3. 验证

- [x] 3.1 `go build` / `go test` 通过
- [x] 3.2 前端 lint/typecheck/build 通过
- [x] 3.3 在 k3s 上验证 cordon/uncordon 往返；drain 的过滤与 PDB 行为由单测覆盖（单节点测试集群不执行整机驱逐；审计落库需数据库环境）