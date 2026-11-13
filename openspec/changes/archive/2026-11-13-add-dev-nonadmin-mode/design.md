## Context

开发模式的身份在 `internal/app/app.go` 中构造，`PlatformAdmin` 固定为 `true`；配置层（`internal/config/config.go`）已有 `dev_kubeconfig`、`dev_cluster_name`、`dev_user` 三个开发专用开关，命名与解析方式可直接沿用。

## Goals / Non-Goals

**Goals:**

- 本地可以构造「非平台管理员 + 指定 Impersonation 用户 + 数据库角色绑定」的组合，用于验收按权限收敛的行为。
- 默认行为与现状完全一致，不改变生产路径。

**Non-Goals:**

- 不引入新的认证方式，不做登录页或用户切换。
- 不在开发模式中绕过 apiserver 的 RBAC 判定。
- 不改变生产配置校验规则。

## Decisions

- **用布尔配置而非新认证模式**：`dev_platform_admin` 只是一个身份属性开关，默认 `true`；比新增 `dev_auth_mode` 之类枚举更小、更直白。
- **环境变量解析严格**：`KUBEDECK_DEV_PLATFORM_ADMIN` 只接受 `true`/`false`（大小写不敏感），其它值启动即报错，避免拼写错误被静默当成 true。
- **仅 `dev_kubeconfig` 模式读取**：该字段不参与生产校验；未配置开发集群时即使设置了也无效果。

## Risks / Trade-offs

- [误在生产设置该变量] → 仅 dev 模式生效，生产 OIDC 路径根本不读取该字段；文档标注"仅本地开发"。
- [默认值反转导致行为变化] → 默认 `true`，与现状一致；配置解析单测锁定默认值。
- [非管理员开发身份没有绑定导致全部 403] → 这是预期行为，文档说明需先通过管理端写入角色绑定。