## Why

集群总览目前只有容量与对象计数（节点可分配、Pod 状态、工作负载异常），用户无法回答「现在到底用了多少 CPU/内存、是否接近打满」。P0 的容量是静态值，P1 的审计统计只覆盖平台操作，缺少运行时的资源消耗视角。本地 k3s 已内置 metrics-server，集群指标 API 可用，补齐用量读取的成本低，也不需要新增存储。

## What Changes

- 总览接口 `GET /api/v1/clusters/{cluster}/overview` 增加 `usage` 字段：集群级可见用户返回节点实际用量、可分配容量与使用率；命名空间受限用户返回其可见命名空间内 Pod 用量合计。
- 集群指标 API（`metrics.k8s.io`）不可用、无权限或读取失败时，`usage.available=false` 并在 `unavailable` 中标注，总览其余数据照常返回，不引入新的错误路径。
- 用量一律通过 Impersonation 以当前用户身份读取；节点级用量仅在用户可读节点指标时返回，受限用户不会看到节点容量或跨命名空间汇总。
- 总览页增加「资源用量」卡片：当前 CPU/内存、使用率进度条，以及本次会话内累积的采样趋势（内存中保留最近约 10 分钟，最多 20 个点）。
- 用量采样 MUST NOT 写入 PostgreSQL，也不新增服务端历史缓冲；趋势由页面在会话内累积，刷新后重新开始。
- 接口契约文档与权限矩阵补充指标读取说明；不引入图表库。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `cluster-overview`: 新增「资源用量与趋势」要求，定义用量口径、授权收敛、指标不可用时的降级、趋势的会话内语义与不落库约束。

## Impact

- 后端：`internal/kube/metrics.go`（指标读取与汇总）、`internal/kube/overview.go`（`Usage` 字段与采集编排）、`internal/api/overview.go`（响应映射与降级）；复用 `Transport` 与现有 `OverviewOptions` 范围。
- 接口契约：`docs/openapi.yaml` 增加 `ClusterUsage` schema 与 overview 响应字段。
- 前端：`web/src/lib/api.ts` 类型、`web/src/features/overview/OverviewPage.tsx` 用量卡片与会话内趋势、`useOverview.ts` 采样累积。
- 数据库：无 schema 变更；用量不入库。
- 依赖：无新增（指标走集群 API，趋势用内联 SVG 绘制）。