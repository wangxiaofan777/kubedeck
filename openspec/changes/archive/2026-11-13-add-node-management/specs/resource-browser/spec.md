## ADDED Requirements

### Requirement: 节点视图

系统 SHALL 提供节点列表与节点详情，展示就绪状态、角色、版本、可分配 CPU/内存、Taints、Labels、Conditions 与该节点上的 Pod。节点列表 MUST 明确标识已停止调度的节点。

#### Scenario: 查看节点列表

- **WHEN** 用户打开节点页面
- **THEN** 列表 SHALL 展示每个节点的角色、状态、版本、可分配资源、Pod 数与是否可调度

#### Scenario: 查看节点上的 Pod

- **WHEN** 用户打开某节点详情
- **THEN** 系统 SHALL 展示该节点上的全部 Pod，并可跳转到 Pod 详情

#### Scenario: 指标不可用

- **WHEN** 集群没有 metrics API
- **THEN** 节点页面 SHALL 仍展示可分配资源与状态，并标注实际用量不可用

### Requirement: 节点调度控制

系统 SHALL 支持 cordon 与 uncordon，操作 MUST 经目标集群 apiserver 以当前用户身份鉴权，并写入审计。

#### Scenario: 停止调度

- **WHEN** 用户对节点执行 cordon
- **THEN** 节点 `spec.unschedulable` SHALL 变为 true，界面 SHALL 显示「已停止调度」

#### Scenario: 恢复调度

- **WHEN** 用户对已 cordon 的节点执行 uncordon
- **THEN** 节点 SHALL 恢复可调度，界面状态同步更新

#### Scenario: 无权限

- **WHEN** 用户没有节点 patch 权限
- **THEN** 系统 SHALL 返回 403，且不修改节点

### Requirement: 节点驱逐

系统 SHALL 支持 drain，并 MUST 默认跳过 DaemonSet 管理的 Pod 与镜像 Pod。对无控制器管理的 Pod 或使用 emptyDir 的 Pod，系统 MUST 默认阻止并要求显式确认。驱逐 MUST 受 PodDisruptionBudget 约束，失败项 SHALL 逐项返回原因，MUST NOT 因单个失败中断全部。

#### Scenario: 正常驱逐

- **WHEN** 用户对节点执行 drain 且所有 Pod 可安全驱逐
- **THEN** 系统 SHALL 逐 Pod 创建 Eviction 请求，并返回成功数量与逐项结果

#### Scenario: PDB 阻止

- **WHEN** 某 Pod 的 PDB 不允许继续中断
- **THEN** 该 Pod SHALL 标记为 blocked，其他 Pod SHALL 继续处理，响应 SHALL 包含 PDB 名称与允许中断数

#### Scenario: 未受控 Pod

- **WHEN** 节点上存在没有控制器 Owner 的 Pod 且未显式强制
- **THEN** 系统 SHALL 阻止该 Pod 的驱逐并说明原因

#### Scenario: 本地存储

- **WHEN** Pod 使用 emptyDir 且未显式允许删除本地数据
- **THEN** 系统 SHALL 阻止该 Pod 的驱逐并说明原因

#### Scenario: 操作审计

- **WHEN** drain 完成
- **THEN** 系统 SHALL 写入一条审计记录，包含节点、成功/跳过/失败数量与操作者