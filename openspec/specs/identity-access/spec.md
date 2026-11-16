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

### Requirement: 登录方式探测

系统 SHALL 提供无需认证的登录方式探测能力，返回当前部署的登录模式（OIDC、开发模式或未配置）以及可安全公开的登录提示信息。该响应 MUST NOT 包含客户端密钥、会话、令牌或任何集群凭据。

#### Scenario: OIDC 模式

- **WHEN** 浏览器在未登录状态请求登录方式
- **THEN** 系统 SHALL 返回 `oidc` 模式与可用的登录入口，登录页 SHALL 渲染 OIDC 登录按钮

#### Scenario: 开发模式

- **WHEN** 浏览器在未登录状态请求登录方式，且服务端启用了开发模式
- **THEN** 系统 SHALL 返回 `dev` 模式与开发身份提示，登录页 SHALL 渲染开发登录入口，MUST NOT 自动建立会话

#### Scenario: 未配置登录方式

- **WHEN** 服务端既未配置 OIDC 也未启用开发模式
- **THEN** 系统 SHALL 返回 `unconfigured` 模式，登录页 SHALL 展示配置缺失说明与管理员处理建议，MUST NOT 渲染无效的登录按钮

### Requirement: 开发模式显式登录

开发模式 SHALL 要求用户通过显式操作建立本地会话；未完成该操作前，受保护接口 MUST 返回 `UNAUTHENTICATED`。开发登录 MUST 仅在开发模式启用时可用，且 MUST 使用与其他会话相同的 HttpOnly、SameSite=Lax Cookie 规则。

#### Scenario: 未登录访问受保护接口

- **WHEN** 开发模式下浏览器尚未建立会话就请求受保护接口
- **THEN** 系统 SHALL 返回 401 `UNAUTHENTICATED`，MUST NOT 隐式创建会话

#### Scenario: 完成开发登录

- **WHEN** 用户在开发模式登录页点击开发登录
- **THEN** 系统 SHALL 建立本地会话并返回当前开发身份，后续请求 SHALL 携带该会话

#### Scenario: 非开发模式调用开发登录

- **WHEN** OIDC 模式或未配置模式下调用开发登录接口
- **THEN** 系统 SHALL 返回 404 或 403，MUST NOT 建立任何会话

### Requirement: 未登录重定向

控制台受保护路由 SHALL 在未登录时重定向到登录页，并携带原始站内地址作为 `returnTo`。登录成功后 SHALL 返回该地址；`returnTo` MUST 只接受站内相对路径，外部地址 MUST 被拒绝。

#### Scenario: 打开受保护深链接

- **WHEN** 未登录用户直接打开某个资源详情链接
- **THEN** 浏览器 SHALL 被重定向到 `/login?returnTo=<原地址>`，登录成功后 SHALL 回到该资源详情

#### Scenario: 外部 returnTo

- **WHEN** 登录请求携带指向外部站点的 `returnTo`
- **THEN** 系统 SHALL 忽略该值并回到控制台首页，MUST NOT 跳转到外部站点

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

### Requirement: OIDC 身份声明校验

平台在建立登录会话前 SHALL 校验 ID Token 的身份声明。当 ID Token 携带非空 `email` 且未显式配置允许未验证邮箱时，`email_verified` MUST 为 `true`；缺失或为 `false` 时登录 MUST 失败，MUST NOT 建立会话或把该邮箱作为集群身份。

平台 MUST 在把 `groups` 声明用于会话与 Kubernetes impersonation 前完成规范化：去除首尾空白、丢弃空值、去重，并限制组数量与单个组名长度。规范化后的组 SHALL 保持首次出现顺序。

#### Scenario: 邮箱已验证

- **WHEN** IdP 返回 `email` 且 `email_verified=true`
- **THEN** 平台 SHALL 建立会话，并以该邮箱作为 `Impersonate-User`

#### Scenario: 邮箱明确未验证

- **WHEN** IdP 返回 `email` 且 `email_verified=false`
- **THEN** 登录 SHALL 失败并返回可定位的认证错误，MUST NOT 建立会话

#### Scenario: IdP 未返回验证状态

- **WHEN** IdP 返回 `email` 但没有 `email_verified` 声明
- **THEN** 默认配置下登录 SHALL 失败；仅当部署显式允许未验证邮箱时才 SHALL 继续

#### Scenario: 组声明包含空值与重复项

- **WHEN** IdP 的 `groups` 声明包含空白、空字符串或重复组名
- **THEN** 平台 SHALL 仅保留去空白、去重后的非空组名，并用于 `Impersonate-Group`

#### Scenario: 组声明超过上限

- **WHEN** IdP 返回的组数量或单个组名长度超过平台上限
- **THEN** 平台 SHALL 丢弃超出部分并记录告警，MUST NOT 把未经限制的声明发送到目标集群
