## Why

代码审查确认了三个会在生产环境造成安全或可用性问题、但没有被前两轮修复覆盖的点：

1. `OIDCClaims` 解析了 `email_verified`，但登录流程从未使用它。IdP 明确返回“邮箱未验证”时，平台仍会建立会话，并把该邮箱作为 `Impersonate-User` 访问集群。
2. `groups` 声明被原样写入会话并用于 `Impersonate-Group`，没有去空白、去重、空值过滤或数量上限；异常或超大的声明会直接进入每个集群请求。
3. Helm 允许 `replicaCount > 1`，但会话与 OIDC 访问令牌只存在进程内存中。多副本下请求被负载均衡到另一个副本后会随机返回 401，而部署清单没有任何阻止或提示。

这些是现有安全与部署契约的缺口，不是新功能。

## What Changes

- OIDC 登录默认要求：当 ID Token 携带 `email` 时，`email_verified` 必须为 `true`；提供显式配置项允许在 IdP 不返回该声明时关闭校验。
- 对 `groups` 声明做规范化：去除首尾空白、丢弃空字符串、去重、限制数量与单项长度，再用于会话与 impersonation。
- Helm 部署模板拒绝 `replicaCount > 1`，并给出明确原因；`values.yaml` 与部署文档说明当前为单副本约束。
- 更新 OIDC 与部署文档，写清 IdP 声明要求、关闭邮箱校验的风险与多副本限制。
- 为声明校验、组规范化和 Helm 约束补充测试。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `identity-access`: 明确 OIDC 登录必须校验邮箱验证状态，并要求组声明在用于授权前完成规范化。
- `engineering-baseline`: 明确会话必须跨副本一致；在只有进程内会话时，部署清单必须拒绝多副本。

## Impact

- 后端：`internal/auth/oidc.go`、`internal/auth/oidc_test.go`、`internal/config/config.go`、`internal/config/config_test.go`、`internal/app/app.go`。
- 部署：`deploy/helm/kubedeck/templates/deployment.yaml`、`deploy/helm/kubedeck/values.yaml`。
- 文档：`docs/deployment.md`、`deploy/compose/README.md`（补充 OIDC 声明与单副本说明）。
- 不改变 API 路径、数据库结构、会话存储实现或前端行为。
- 默认开启邮箱校验会让不返回 `email_verified` 的 IdP 登录失败；部署文档提供显式关闭项。