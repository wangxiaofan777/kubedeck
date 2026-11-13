## Why

创建弹窗虽然允许编辑整段 YAML，但提交时固定使用「当前资源页」的集合路径。用户在 Deployments 页把 `kind` 改成 ConfigMap 时，服务端按路径与请求体一致性校验返回 422「apiVersion 与请求路径不一致」；这是正确的服务端守卫，但界面在提交前没有任何提示，用户只能通过报错发现「本页只能建本页类型」。

后端已经支持向任意已发现资源的集合接口创建对象，缺口只在前端：没有按 YAML 的 `apiVersion`/`kind` 解析目标资源并选择正确的集合路径。

## What Changes

- 创建弹窗提交前解析 YAML 的 `apiVersion`、`kind`、`metadata.name` 与 `metadata.namespace`，通过资源发现解析目标资源（含 CRD），并使用目标资源的集合路径创建，而不是固定使用当前页面类型。
- YAML 的 `kind` 与当前页面不同时，弹窗内提前提示「将创建 X 并跳转到对应页面」；创建成功后按目标类型跳转详情页；控制台没有对应列表页的类型（未内置的 CRD）则停留在当前页并提示创建结果。
- 目标类型无法从资源发现中解析、或 YAML 与目标资源的命名空间规则冲突时，在客户端直接给出明确错误，不发起请求。
- 服务端保持一致性与权限校验：路径与请求体不一致仍返回 422，`create` 权限仍由 apiserver 判定；前端解析不构成授权。
- 不新增后端接口，不改变现有创建 API 的语义。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `resource-browser`: 新增「创建目标类型按 YAML 解析」要求，定义目标资源解析、跨类型跳转、无法解析时的本地拒绝与权限边界。

## Impact

- 前端：新增资源发现查询 hook；`ResourceCreateModal` 增加目标解析、跨类型提示与按目标创建；`ResourcesPage` 的创建成功回调按目标类型导航；`resourceTypes` 提供 GVR → 页面类型的反查。
- 后端：无代码变更；复用 `GET /api-resources` 与既有集合创建接口。
- 文档：无接口契约变更；`docs/local-environment.md` 不需要调整。
- 依赖：无新增。