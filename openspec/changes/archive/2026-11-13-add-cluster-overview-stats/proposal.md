## Why

控制台目前没有任何面向用户的统计：登录后直接落到第一个集群的 Pod 列表，用户无法在进入系统时判断「集群现在是否健康、有多少工作负载异常」。设计文档 `k8s-console-design.md` 第 614 行规划的 `/clusters/:cluster/overview`（节点、容量、异常工作负载）属于 Phase 1 范围，但至今未实现；`/clusters` 接口也只返回名称、版本与状态，不含任何资源计数。现在补齐第一屏的只读总览，成本低且能显著改善可观测性，也是后续指标趋势图（P2）的落点。

## What Changes

- 新增聚合读接口 `GET /api/v1/clusters/{cluster}/overview`：一次返回节点状态与可分配容量、命名空间数、Pod 状态分布、异常工作负载计数、最近事件摘要。
- 新增控制台「集群总览」页面：作为进入某个集群后的默认落地页，替换当前直接跳到 Pod 列表的行为。
- 统计**只读、不落库**：集群资源计数实时从 apiserver 按当前用户身份读取，不写入 PostgreSQL，不新增存储。
- 统计**按权限范围收敛**：只计入当前用户有权读取的资源；无权限的命名空间既不计入数量也不暴露存在性。部分资源读取失败时降级返回可用部分并显式标注，不能因一项失败让整页不可用。
- 审计统计（P1）与指标趋势图（P2）不在本次范围，本次只做 P0。

## Capabilities

### New Capabilities

- `cluster-overview`: 定义集群总览的聚合口径、权限边界、降级行为与页面呈现要求。

### Modified Capabilities

（无）

## Impact

- 后端：新增 `internal/kube/overview.go`（基于 `Transport` 的聚合逻辑，可单测）、`internal/api/overview.go`（HTTP 处理器），在 `internal/api/router.go` 注册路由；复用现有 Impersonation 传输层，不新增集群访问通道。
- 接口契约：`docs/openapi.yaml` 增加 `/clusters/{cluster}/overview`；`internal/api/openapi_test.go` 的路由一致性检查需同步。
- 前端：新增 `web/src/features/overview/`（页面与数据 hook），`web/src/App.tsx` 增加路由并把默认落地改为总览，`web/src/app/ConsoleShell.tsx` 增加「总览」菜单项；不引入图表库等新依赖。
- 权限：完全依赖 apiserver 对 impersonate 身份的 RBAC 判定；平台不维护第二套权限表。
- 数据库：无 schema 变更，不新增表。
- 依赖：无新增第三方依赖。