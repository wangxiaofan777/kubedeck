## Why

资源浏览器目前只能查看、全量更新和删除已有对象，无法创建新对象，也没有部分更新能力。平台传输层已实现 `Create`，但没有任何 HTTP 路由调用它；`PATCH` 在传输层、接口契约和前端完全缺失。这导致控制台无法完成"新建工作负载 / Namespace / CRD 实例"这类日常操作，与 `k8s-console-design.md` 附录 D 声明的 `POST` 创建、`PATCH` 合并/策略合并/SSA 契约不一致，也让审计规格中的 `create`/`patch` 动词没有真实入口。

## What Changes

- 新增 `POST /api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}`：创建任意已发现资源（含 CRD），支持 JSON/YAML 请求体、`dryRun=All`、命名空间与集群级资源的正确区分，返回 201 与创建后的对象。
- 新增 `PATCH /api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}/{name}`：支持 `application/merge-patch+json`、`application/strategic-merge-patch+json`、`application/apply-patch+yaml`（Server-Side Apply），支持 `dryRun`、`fieldManager`、`force`。
- `Transport` 增加 `Patch` 能力；创建路径接通既有的 `Transport.Create`。
- 审计正确记录 `create`/`patch` 动词（当前 PATCH 不在动词映射中，创建冲突会被错误归类为 500）。
- `docs/openapi.yaml` 补齐两个操作，并通过路由一致性测试。
- 前端资源列表页增加"新建"入口：按当前资源类型预填 `apiVersion`/`kind`/`metadata`，支持本地 YAML 校验、服务端 `dryRun` 校验与提交；无 `create` 权限的用户不展示入口。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `resource-browser`: 新增"资源创建"与"部分更新（PATCH / Server-Side Apply）"两项需求，并把只读用户限制扩展到创建入口。

## Impact

- 后端：`internal/api/router.go`、`internal/api/resources_write.go`（或新增 `resources_create.go`）、`internal/api/audit.go`、`internal/api/errors.go`、`internal/kube/transport.go`、`internal/kube/resources.go`、`internal/api/handlers_test.go`、`internal/api/openapi_test.go`。
- 接口契约：`docs/openapi.yaml` 增加 `POST`（资源集合）与 `PATCH`（单资源）。
- 前端：`web/src/lib/api.ts`、`web/src/features/resources/ResourcesPage.tsx`、YAML 编辑器复用（新建模式）、`useCan` 预检。
- 权限：写操作仍完全依赖 apiserver 对 impersonate 身份的 RBAC 判定；平台不新增权限表。创建/补丁需要 `create` / `patch` 权限。
- 文档：`docs/permission-matrix.md` 中关于 `create`/`patch` 的说明将与实现对齐。
- 无数据库变更；不引入新依赖；不改变既有读、更新、删除接口语义。