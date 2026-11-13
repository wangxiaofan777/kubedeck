## ADDED Requirements

### Requirement: 命名空间可见性

系统 SHALL 以平台绑定划定用户可见的命名空间范围，并 SHALL 允许在目标集群上有任一绑定的用户访问该集群的发现、详情与健康信息；仅当请求跨越全部命名空间时才要求 `*` 绑定。

#### Scenario: 按命名空间绑定

- **WHEN** 用户只被绑定到 `prod` 集群的 `default` 与 `dev` 命名空间
- **THEN** 命名空间列表 SHALL 返回 `default` 与 `dev`（去重、排序后），且 MUST NOT 返回 403

#### Scenario: 全部命名空间绑定

- **WHEN** 用户对 `prod` 集群拥有 `*` 绑定
- **THEN** 系统 SHALL 以该用户身份向目标集群查询命名空间，并只返回 apiserver 允许其查看的命名空间

#### Scenario: 平台管理员

- **WHEN** 平台管理员请求某集群的命名空间列表
- **THEN** 系统 SHALL 返回该集群内全部命名空间

#### Scenario: 无任何绑定

- **WHEN** 用户对目标集群没有任何平台绑定
- **THEN** 命名空间列表 SHALL 返回 403，且响应 MUST NOT 泄露该集群的命名空间名称

#### Scenario: 集群级接口准入

- **WHEN** 只绑定到具体命名空间的用户请求资源发现、集群详情或集群健康
- **THEN** 系统 SHALL 正常返回结果；凭据信息仍 MUST 仅对平台管理员可见

#### Scenario: 回收绑定

- **WHEN** 管理员删除用户在某命名空间的绑定
- **THEN** 该用户后续的命名空间列表 SHALL 在 60 秒内不再包含该命名空间