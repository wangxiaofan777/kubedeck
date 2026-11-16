## ADDED Requirements

### Requirement: 命名空间单一来源

涉及具体对象的操作 SHALL 只使用一个命名空间值。路径中的命名空间与查询参数不一致时，系统 MUST 返回 400 `INVALID_ARGUMENT`，MUST NOT 用其中一个做准入检查、用另一个执行操作。

#### Scenario: 参数不一致

- **WHEN** 用户请求 `/clusters/prod/pods/kube-system/foo/log?namespace=team-a`
- **THEN** 系统 SHALL 返回 400，且不向目标集群发起任何请求

#### Scenario: 平台绑定与实际对象一致

- **WHEN** 用户只绑定了 `team-a`，请求 `team-a` 下 Pod 的日志且未携带 query 参数
- **THEN** 平台准入与实际读取 SHALL 都作用于 `team-a`

### Requirement: 上游错误脱敏

返回给客户端的错误 MUST NOT 包含 apiserver 的原始响应、内部地址、RBAC 角色名或证书细节。系统 SHALL 返回稳定错误码与脱敏消息，并将原始错误写入服务端日志用于排障。

#### Scenario: apiserver 拒绝请求

- **WHEN** apiserver 返回 403 并带有内部角色信息
- **THEN** 客户端 SHALL 只收到 `FORBIDDEN` 与通用说明，原始内容 SHALL 只出现在服务端日志

#### Scenario: 集群不可达

- **WHEN** 连接目标集群失败
- **THEN** 客户端 SHALL 收到 `CLUSTER_UNREACHABLE`，消息 MUST NOT 包含内部 IP 或 DNS 细节