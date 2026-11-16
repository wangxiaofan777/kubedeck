## ADDED Requirements

### Requirement: 会话身份刷新

每次已认证请求 SHALL 从平台数据库刷新用户的启用状态与平台管理员标记；用户被禁用后 MUST 在下一个请求即被拒绝，MUST NOT 等到会话自然过期。组信息 MAY 保持登录时快照，但会话有效期 SHALL 有上限。

#### Scenario: 管理员被取消

- **WHEN** 管理员在数据库中取消某用户的平台管理员标记
- **THEN** 该用户的下一个请求 SHALL 不再拥有管理员权限

#### Scenario: 用户被禁用

- **WHEN** 用户状态被改为 `disabled`
- **THEN** 该用户的已有会话 SHALL 在下一个请求返回 401，并终止实时连接

### Requirement: 平台角色动词约束

平台角色 SHALL 同时约束可见范围与可执行动词：`viewer` MUST 只能执行只读操作，写请求 MUST 被平台拒绝；`developer` SHALL 允许在绑定范围内读写。目标集群 RBAC 仍是最终授权来源，平台角色不得放宽其限制。

#### Scenario: 只读用户直接调用写接口

- **WHEN** 只绑定 `viewer` 的用户绕过界面直接调用删除接口
- **THEN** 平台 SHALL 返回 403 `FORBIDDEN`，且不向 apiserver 发起删除请求

#### Scenario: developer 写操作

- **WHEN** `developer` 在绑定命名空间内创建资源且目标集群 RBAC 允许
- **THEN** 请求 SHALL 正常执行