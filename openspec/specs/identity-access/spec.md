# identity-access Specification

## Purpose
定义用户如何登录 KubeDeck、平台如何以用户身份访问集群，以及权限在平台侧与 Kubernetes 侧如何划分，确保集群原生 RBAC 是授权的唯一事实来源。

## Requirements

### Requirement: OIDC 登录

系统 SHALL 通过 OIDC 授权码 + PKCE 完成登录；会话凭据 MUST 存放于 `HttpOnly`、`Secure`、`SameSite=Lax` 的 Cookie 中，且 MUST NOT 出现在 URL 或前端可读存储中。

#### Scenario: 登录成功

- **WHEN** 用户通过企业 IdP 完成授权码流程
- **THEN** 系统 SHALL 建立会话并在 3 秒内返回当前用户与其可访问的集群范围

#### Scenario: 拒绝未授权来源

- **WHEN** 登录回调的 `state` 与发起登录时不匹配
- **THEN** 系统 SHALL 拒绝该次登录并返回 400，不建立会话

### Requirement: 身份透传

系统 MUST 以当前登录用户的身份访问目标集群 apiserver，采用 Impersonation 或 OIDC Token 透传；MUST NOT 以平台自身的高权限身份代替用户执行用户发起的资源操作。

#### Scenario: 越权请求由 apiserver 拒绝

- **WHEN** 用户请求删除其无权删除的 Deployment
- **THEN** 系统 SHALL 返回 403，并透传 apiserver 给出的拒绝原因

#### Scenario: 审计记录真实身份

- **WHEN** 平台以 Impersonation 代用户发起请求
- **THEN** 审计记录中的操作者 SHALL 是被模拟的真实用户，而不是平台 ServiceAccount

### Requirement: 权限预检

系统 SHALL 提供批量权限预检能力，返回每个「集群 × 命名空间 × 动作 × 资源」组合的 `allowed` 与 `reason`；预检结果的缓存时间 MUST NOT 超过 60 秒。

#### Scenario: 预检命中 RBAC

- **WHEN** 用户查询某命名空间内 Pod 的删除权限
- **THEN** 系统 SHALL 返回是否允许，并在拒绝时给出可读原因（如绑定的 RoleBinding 名称）

#### Scenario: 缓存过期

- **WHEN** 角色绑定发生变更且超过缓存有效期
- **THEN** 后续预检 SHALL 反映最新权限，不得继续返回旧结果

### Requirement: 服务端强制鉴权

所有资源请求 MUST 在服务端重新校验权限；前端隐藏操作 MUST NOT 被视为授权依据。

#### Scenario: 绕过前端直接调用

- **WHEN** 用户绕过界面，直接构造其无权执行的写请求
- **THEN** 系统 SHALL 拒绝该请求，行为与界面隐藏时一致

### Requirement: 角色绑定

系统 SHALL 支持将平台角色绑定到「用户 × 集群 × 命名空间」；绑定的新增或回收 SHALL 在 60 秒内对访问控制生效。

#### Scenario: 授予权限

- **WHEN** 管理员为开发者授予 `prod` 集群 `default` 命名空间的 `Developer` 角色
- **THEN** 该用户 SHALL 在 60 秒内看到该集群与命名空间，并可执行角色允许的操作

#### Scenario: 回收权限

- **WHEN** 管理员删除该绑定
- **THEN** 该用户后续的请求 SHALL 在 60 秒内被拒绝，已打开的实时订阅 SHALL 被终止

### Requirement: 会话终止

系统 SHALL 在用户主动登出或 IdP 撤销会话后 5 分钟内使其平台会话失效。

#### Scenario: 主动登出

- **WHEN** 用户点击登出
- **THEN** 会话 SHALL 立即失效，后续请求返回 401

#### Scenario: IdP 侧撤销

- **WHEN** 用户在 IdP 侧被禁用或撤销授权
- **THEN** 平台会话 SHALL 在 5 分钟内失效

### Requirement: 初始管理员

系统 SHALL 支持通过一次性引导流程创建首个平台管理员，且该流程在首个管理员创建后 MUST 自动失效。

#### Scenario: 首次引导

- **WHEN** 系统中尚不存在平台管理员
- **THEN** 持有引导令牌的管理员 SHALL 能创建首个平台管理员账号

#### Scenario: 引导流程关闭

- **WHEN** 已存在至少一个平台管理员
- **THEN** 再次调用引导接口 SHALL 返回 409，且不创建任何账号

### Requirement: IdP 故障时的行为

身份提供方不可用时，系统 MUST NOT 静默降级为本地密码登录；已建立的会话 SHALL 在有效期内继续可用，新登录 SHALL 明确失败。

#### Scenario: IdP 不可用但已有会话

- **WHEN** IdP 暂时不可达，用户持有未过期的平台会话
- **THEN** 该用户的正常请求 SHALL 继续成功，不因 IdP 故障被登出

#### Scenario: IdP 不可用时的新登录

- **WHEN** 用户在 IdP 不可达期间尝试登录
- **THEN** 系统 SHALL 返回明确的 `UNAUTHENTICATED` 与原因，不得回退到本地密码校验

#### Scenario: 并发会话上限

- **WHEN** 同一用户的活跃会话数超过配置上限
- **THEN** 系统 SHALL 使最早建立的会话失效，并在新会话中提示

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
