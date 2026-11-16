## MODIFIED Requirements

### Requirement: Pod 删除与驱逐

系统 SHALL 支持删除单个或多个 Pod，并支持驱逐（Eviction）；驱逐遇到 PodDisruptionBudget 阻止时 MUST 返回阻止原因。系统 SHALL 根据目标集群实际服务的 Eviction API 版本自动选择请求版本，MUST NOT 要求用户手工指定。

#### Scenario: 驱逐被 PDB 阻止

- **WHEN** 用户驱逐一个受 PDB 保护的 Pod
- **THEN** 系统 SHALL 返回 PDB 名称与当前允许中断数量，且不强制删除 Pod

#### Scenario: 仅提供 policy/v1beta1 的集群

- **WHEN** 用户在只提供 `policy/v1beta1` Eviction 的集群上驱逐 Pod
- **THEN** 系统 SHALL 使用该版本提交驱逐请求，并在 PDB 允许时成功驱逐

#### Scenario: 删除由控制器管理的 Pod

- **WHEN** 用户删除属于 Deployment 的 Pod
- **THEN** 系统 SHALL 提示该 Pod 会被控制器重建，并在确认后执行