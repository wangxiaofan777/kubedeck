# 提案：增加节点管理能力

## 背景

节点是 K8s 运维的第一现场，但当前 Nodes 只是一个只读资源类型：列表里一列状态，详情页是通用 Descriptions + YAML。没有 cordon/uncordon、没有 drain、没有「节点上有哪些 Pod」、没有容量与 Conditions 视图。对「集群驾驶舱」来说这是明显缺口。

## 目标

1. 节点列表：名称、角色、状态、版本、可分配 CPU/内存、Pod 数、是否可调度、Age。
2. 节点详情：Conditions、Capacity/Allocatable、Taints、Labels、该节点上的 Pod 列表。
3. 安全操作：cordon、uncordon、drain（可配置是否忽略 DaemonSet、是否强制、宽限期），并逐 Pod 返回结果。
4. drain 必须考虑 PDB 与本地存储：PDB 阻止时明确失败；未受控 Pod 或使用 emptyDir 的 Pod 默认拒绝，需显式确认。
5. 所有操作经 Impersonation 由 apiserver 鉴权，并写入审计。

## 非目标

- 不做节点加入/移除（kubeadm/云厂商生命周期）。
- 不做 kubelet 重启、节点修复、SSH。
- 不实现节点指标历史；仅展示当前 metrics（若可用）。

## 影响

- `internal/kube`：新增节点操作函数与 drain 编排。
- `internal/api`：新增节点操作路由与审计动词。
- `web`：新增节点列表/详情页面与操作 Drawer。
- 复用现有 `Transport` 的 Get/List/Patch/Evict，不改变授权模型。