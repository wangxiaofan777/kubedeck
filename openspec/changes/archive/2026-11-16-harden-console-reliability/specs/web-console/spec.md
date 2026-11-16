## ADDED Requirements

### Requirement: 状态语义判定

状态徽标的颜色判定 SHALL 优先识别失败与危险状态，再识别进行中与成功状态。包含 `NotReady`、`CrashLoopBackOff`、`ImagePullBackOff`、`Evicted`、`Failed` 等子串的状态 MUST 展示为危险色，MUST NOT 因状态文本同时包含 `Ready` 或 `Complete` 等成功关键词而被判为成功。

#### Scenario: NotReady 容器

- **WHEN** 容器状态为 `NotReady`
- **THEN** 状态徽标 SHALL 使用危险色，并保留可读的状态文本

#### Scenario: 正常状态

- **WHEN** 状态为 `Running` 或 `Ready`
- **THEN** 状态徽标 SHALL 使用成功色

#### Scenario: 未知状态

- **WHEN** 状态文本不匹配任何已知规则
- **THEN** 状态徽标 SHALL 使用中性色，而不是默认成功色

### Requirement: 上下文切换一致性

集群列表在接入或删除集群后 SHALL 使集群缓存失效。用户切换集群时 SHALL 进入新集群的总览页，MUST NOT 保留旧集群的对象详情路径、对象名称或命名空间查询参数；用户随后可自行选择新集群中的目标资源。

#### Scenario: 接入集群后打开设置

- **WHEN** 用户完成集群接入并被导航到设置页
- **THEN** 集群缓存 SHALL 已刷新，页面 MUST NOT 展示“集群不存在”

#### Scenario: 删除集群后返回列表

- **WHEN** 用户删除当前集群并返回集群列表
- **THEN** 列表 SHALL 不再展示已删除集群

#### Scenario: 切换集群

- **WHEN** 用户从 Pod 详情页切换到另一个集群
- **THEN** 浏览器 SHALL 进入新集群的总览页，MUST NOT 尝试打开新集群中同名的 Pod 或旧命名空间