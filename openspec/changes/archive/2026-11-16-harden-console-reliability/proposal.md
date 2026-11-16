## Why

对照当前实现做了一轮代码审查，发现若干会直接影响可用性的缺陷：实时订阅在服务端下发 `resync` 后不会重建，列表从此不再实时更新；权限预检使用 `scale`/`restart`/`evict` 等 Kubernetes RBAC 不存在的动词，导致合法编辑者看不到伸缩、重启、驱逐入口；节点驱逐受 30 秒全局请求超时截断；`NotReady` 被状态颜色规则误判为成功；集群接入/删除后缓存不失效；数据库迁移测试的期望已过期，导致 `make test-db` 与 CI 的 backend job 必然失败。

这些不是新功能，而是让现有能力按规格工作所必需的修复。

## What Changes

- 实时订阅：客户端收到 `resync` 后重新订阅；订阅请求携带标签选择器，过滤列表不再混入不匹配对象。
- 权限预检：新增子资源参数；伸缩改为检查 `update` + `scale` 子资源，重启与回滚改为检查 `patch`，Pod 驱逐改为检查 `create` + `eviction` 子资源。
- 节点驱逐：`POST /drain` 不再套用 30 秒普通请求超时，使用操作自身的 120/300 秒超时。
- 状态语义：`NotReady`、`CrashLoopBackOff` 等失败状态优先于成功关键词匹配。
- 上下文一致性：集群接入或删除后刷新集群缓存；切换集群时回到新集群总览，避免携带旧对象路径与命名空间。
- 测试闸门：修正迁移测试的期望迁移列表，使 `make test-db` 与 CI 恢复可信。
- 为状态颜色、实时订阅帧、权限预检动词映射补充可回归的测试。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `realtime-stream`: 明确客户端在收到重新同步指令后必须重建订阅，并要求订阅携带标签选择器。
- `identity-access`: 权限预检必须使用目标集群真实存在的 RBAC 动词与子资源，不得用平台动作名直接查询。
- `resource-browser`: 节点驱逐必须在自身超时内完成，不得被普通请求超时截断。
- `web-console`: 状态语义必须优先识别失败状态；集群切换与集群列表变更后不得展示过期上下文。

## Impact

- 后端：`internal/api/middleware.go`（长请求豁免）、`internal/api/nodes.go` 或超时判断、`internal/store/store_test.go`、相关 handler 测试。
- 前端：`web/src/lib/useRealtime.ts`、`web/src/features/resources/ResourcesPage.tsx`、`web/src/features/auth/useCan.ts`、`web/src/features/workloads/WorkloadActions.tsx`、`web/src/components/StatusPill.tsx`、`web/src/features/clusters/*`、`web/src/app/ConsoleShell.tsx`。
- 不改变 API 路径、数据库结构或 OIDC 登录流程。
- 可能影响依赖 30 秒超时的测试断言，需要同步调整。