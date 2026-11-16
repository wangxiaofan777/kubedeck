## ADDED Requirements

### Requirement: 操作动作的预检动词映射

界面在预检“伸缩、重启、回滚、暂停/恢复、驱逐”等平台动作时，MUST 使用目标集群真实存在的 RBAC 动词与子资源，不得把平台动作名直接作为 SSAR 动词。映射至少满足：

- 伸缩：对工作负载检查 `update`，子资源 `scale`
- 重启、回滚、暂停/恢复：对工作负载检查 `patch`
- Pod 驱逐：对 Pod 检查 `create`，子资源 `eviction`

平台角色仍 SHALL 先约束写操作；映射后的预检结果只用于界面展示，服务端写请求仍 MUST 由平台准入与 apiserver RBAC 再次判定。

#### Scenario: 合法编辑者看到伸缩入口

- **WHEN** 用户拥有目标 Deployment 的 `update`/`deployments/scale` 权限
- **THEN** 伸缩入口 SHALL 可见且可点击，MUST NOT 因使用不存在的 `scale` 动词而被隐藏

#### Scenario: 合法编辑者看到重启入口

- **WHEN** 用户拥有目标工作负载的 `patch` 权限
- **THEN** 重启、历史与回滚入口 SHALL 可见

#### Scenario: 驱逐权限

- **WHEN** 用户拥有 Pod 的 `create`/`pods/eviction` 权限
- **THEN** 驱逐入口 SHALL 可见，点击后服务端仍 SHALL 以真实权限再次校验

#### Scenario: 只读用户

- **WHEN** 用户只有目标资源的 `get`/`list` 权限
- **THEN** 上述写操作入口 SHALL 保持隐藏或禁用