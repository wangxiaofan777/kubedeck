## Why

开发模式把身份硬编码为平台管理员（`PlatformAdmin: true`），本地无法复现「只有某个命名空间绑定」的普通用户。这导致按用户权限范围收敛的功能无法端到端验收——`add-cluster-overview-stats` 的受限账号验收（task 4.4）就卡在这里：只能靠 handler 单测覆盖，无法真实走一遍 Impersonation + 平台绑定 + 命名空间统计。

## What Changes

- 新增开发模式专用配置 `dev_platform_admin`（环境变量 `KUBEDECK_DEV_PLATFORM_ADMIN`），默认 `true`，保持现有行为不变。
- 设为 `false` 时，开发身份不再拥有平台管理员权限：集群/命名空间准入按数据库中的角色绑定执行，同时仍可通过 `dev_user` 指定 Impersonation 身份。
- 仅在使用 `dev_kubeconfig` 的开发模式生效；OIDC 生产路径与既有校验完全不变。
- 文档补充该变量的用途与示例。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

（无，纯开发工具链变更，`.openspec.yaml` 声明 `skip_specs: true`）

## Impact

- 配置：`internal/config/config.go` 增加字段、默认值、环境变量解析与非法值校验。
- 启动：`internal/app/app.go` 用配置值替代硬编码的 `PlatformAdmin: true`。
- 文档：`docs/local-environment.md` 的开发模式变量表与示例。
- 测试：配置解析单测；不改变生产认证、授权与任何 API 契约。