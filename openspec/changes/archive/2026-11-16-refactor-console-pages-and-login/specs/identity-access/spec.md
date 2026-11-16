## ADDED Requirements

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