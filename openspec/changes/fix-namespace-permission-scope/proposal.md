## Why

命名空间可见性与现有权限模型不一致。后端 `listNamespaces` 先经过"集群级准入"（要求 `*` 绑定）再过滤，导致只绑定具体命名空间的用户直接收到 403，后面的按绑定过滤分支对这类用户不可达；`api-resources`、集群详情与健康检查同样要求 `*`，使这类用户连资源发现都拿不到。前端则无条件提供"全部命名空间"选项，并在首次进入时默认按全部命名空间请求，进一步放大 403。这与 `identity-access`（绑定后 60 秒内可见）和 `web-console`（只展示有权限的上下文）的验收标准不符。

## What Changes

- 区分**集群可见性**与**命名空间范围**：用户只要在目标集群上有任一平台绑定，就可以访问集群发现、详情与健康信息；只有跨全部命名空间的请求才要求 `*` 绑定。
- 修复 `GET /clusters/{cluster}/namespaces`：
  - 平台管理员：返回集群内全部命名空间；
  - `*` 绑定：返回以该用户身份在 apiserver 可见的命名空间；
  - 具体命名空间绑定：返回绑定去重后的命名空间列表，不再要求集群级权限；
  - 无绑定：403。
- `GET /api-resources`、`GET /clusters/{cluster}`、`GET /clusters/{cluster}/health` 改为按集群可见性准入，不再要求 `*`。
- 前端命名空间选择器只展示后端返回的可访问命名空间；无集群级范围时不再提供"全部命名空间"，首次进入自动选中首个可访问命名空间；集群级资源类型（Namespaces、Nodes）对这类用户不展示。
- 补充后端与前端测试，覆盖按命名空间绑定、`*` 绑定、无绑定与越权链接。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `identity-access`: 新增"命名空间可见性"要求，明确绑定范围如何决定命名空间列表与集群级接口准入。
- `web-console`: 修改"上下文导航"要求，限定命名空间选择器的内容、默认选择与集群级导航可见性。

## Impact

- 后端：`internal/api/clusters.go`（`listNamespaces`）、`internal/api/access.go`（新增集群可见性判定）、`internal/api/resource_helpers.go`、`internal/api/resources.go`、`internal/api/cluster_admin.go`、相关测试。
- 前端：`web/src/app/ConsoleShell.tsx`（命名空间选项、自动选择、导航过滤）、`web/src/features/namespaces/useNamespaces.ts`、`web/src/lib/api.ts`（如需类型补充）。
- 权限模型不变：仍以平台绑定划定可见范围、以 impersonated 身份由 apiserver 做最终 RBAC 判定；不新增权限表。
- 无数据库变更，无新依赖；`*` 绑定的名字枚举依赖用户在目标集群的 `list namespaces` 权限，文档需说明。
- 开发模式仍固定为 PlatformAdmin，非管理员行为需通过 OIDC 账号验证；单元测试覆盖主要分支。