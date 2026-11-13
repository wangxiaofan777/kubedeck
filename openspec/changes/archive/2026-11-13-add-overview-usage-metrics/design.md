## Context

总览已有聚合采集（`internal/kube/overview.go`）与按用户收敛的入口（`internal/api/overview.go`）。集群指标由 `metrics.k8s.io/v1beta1` 提供（`nodes`/`pods`），本地 k3s 自带 metrics-server，`kubectl top nodes` 可用。用量是运行时数据，不能进平台库，也不能跨用户复用。

## Goals

- 一次总览请求同时返回对象统计与资源用量，前端沿用 30 秒轮询，不增加额外往返。
- 指标不可用或越权时只降级用量区域，不影响总览其余部分。
- 命名空间受限用户只能看到自己可见范围内的 Pod 用量，看不到节点容量与集群级汇总。
- 趋势在会话内可见，但服务端不保存任何历史。

## Non-Goals

- 不接入 Prometheus 或历史时序库，不提供跨会话趋势。
- 不做单 Pod/单容器的用量排行。
- 不新增 `metrics` 相关数据库表或缓存服务。

## Decisions

### 1. 用量并入总览响应，而不是新增独立接口

总览页已有 30 秒轮询，独立接口需要前端协调两个请求的刷新节奏与降级状态。`Overview.Usage` 为空值即表示不可用，前端只需一处渲染分支。

响应结构（`usage` 可为 `null`，此时 `unavailable` 含 `"metrics"`）：

```json
{
  "usage": {
    "available": true,
    "scope": "nodes",
    "fetchedAt": "2026-11-13T08:30:00Z",
    "cpuMilli": 83,
    "memoryBytes": 2956984320,
    "cpuAllocatableMilli": 2000,
    "memoryAllocatableBytes": 6442450944,
    "cpuPercent": 4.15,
    "memoryPercent": 45.9
  }
}
```

- `scope=nodes`：用量来自节点指标汇总，附可分配容量与百分比。
- `scope=pods`：用量来自可见命名空间内 Pod 指标汇总；`cpuAllocatableMilli`/`memoryAllocatableBytes`/百分比为 `null`，因为节点容量对该用户不可见。
- `cpuMilli` 与 `memoryBytes` 为整数；百分比由服务端按可分配值计算并保留两位小数，避免前端口径不一致。

### 2. 采集顺序与授权收敛

复用 `collectOverview` 的并发编排，新增一节 `collectUsage`，与其它统计并行：

1. 无范围限制（`NamespacesKnown=false` 或包含 `*`）：先读节点指标 `nodes`。成功即 `scope=nodes`，同时用已读到的节点 `allocatable` 计算百分比。
2. 节点指标被拒绝（403）或用户范围受限：退回 Pod 指标。范围受限时逐个命名空间读取 `pods` 指标，沿用现有 `listScoped` 的并发信号量与总超时；无范围限制但节点指标被拒时，先尝试跨命名空间 `pods`，被拒再按可见命名空间逐个读取。
3. 两条路径都失败：
   - `IsNotFound`/聚合 API 不存在 → 返回 `ErrMetricsUnavailable`，总览把 `metrics` 加入 `unavailable`，不设错误。
   - 其它错误（超时、5xx）同样降级为不可用，但记录日志字段 `cluster`/`error`，便于排查。
4. 空指标列表（metrics-server 刚启动尚无样本）返回零值用量且 `available=true`，避免页面在冷启动时误报不可用。

节点指标属于集群级数据：只要节点指标读取成功，就说明 apiserver 已按用户权限放行，可以直接返回；不会出现「无节点权限却拿到节点用量」的情况。

### 3. 趋势只在页面会话内累积

服务端保存历史需要按「集群 × 用户 × 授权范围」分桶，否则会把跨命名空间用量泄露给受限用户；引入键值缓存或时序库又超出本期范围。因此：

- `useOverview` 在每次成功响应后把 `usage` 追加到一个 `useRef` 环形数组（最多 20 个点，30 秒一个点，约 10 分钟）。
- 页面用内联 SVG 折线绘制 CPU 与内存趋势，不引入图表库（与审计统计一致）。
- 刷新、切换集群或重新进入页面后趋势清空；卡片上明确标注「本次会话采样」。
- 服务端不新增任何内存/持久化缓冲，多副本部署下也不会出现趋势不一致。

### 4. 降级展示

- `usage` 为空：卡片显示「指标不可用」提示，说明需要集群启用指标 API（metrics-server），保留其余总览内容；不弹全局错误。
- 用户受限且只有 Pod 用量：卡片隐藏容量与百分比，只显示绝对用量，并标注「仅统计可见命名空间」。
- 总览整体 503（集群不可达）时沿用既有错误页，不需要单独处理用量。

## API 契约

`GET /api/v1/clusters/{cluster}/overview` 响应新增：

| 字段 | 类型 | 说明 |
|---|---|---|
| `usage` | object \| null | 资源用量；不可用为 `null` |
| `usage.available` | boolean | 是否采集成功 |
| `usage.scope` | `nodes` \| `pods` | 用量口径 |
| `usage.fetchedAt` | RFC3339 | 指标采样时间 |
| `usage.cpuMilli` | integer | CPU 用量（millicores） |
| `usage.memoryBytes` | integer | 内存用量（bytes） |
| `usage.cpuAllocatableMilli` | integer \| null | 仅 `scope=nodes` |
| `usage.memoryAllocatableBytes` | integer \| null | 仅 `scope=nodes` |
| `usage.cpuPercent` | number \| null | 仅 `scope=nodes` |
| `usage.memoryPercent` | number \| null | 仅 `scope=nodes` |

`unavailable` 为字符串数组，新增取值 `metrics`。

## 关键时序

```
前端 30s 轮询 ──> overview handler
                     ├─ 并行：nodes/pods/workloads/events（既有）
                     └─ collectUsage
                          ├─ nodes metrics 成功 ──> scope=nodes, 附容量与百分比
                          ├─ 403/受限 ──> 逐命名空间 pods metrics ──> scope=pods
                          └─ 全失败 ──> usage=null, unavailable+=metrics
前端收到 usage ──> 更新卡片 + 追加会话采样点 ──> SVG 折线
```

## 性能预算

- 指标读取与既有统计共享总超时（8 秒）与并发上限（8）；节点指标为一次列表调用，受限用户按命名空间逐个读取，上限与现有 fallback 一致。
- 指标列表不设分页读取上限：metrics-server 返回的是当前快照，本地规模下单次响应在百 KB 级；沿用现有 `overviewMaxObjects` 截断保护。
- 总览 P95 目标维持不高于 2 秒（本地 k3s，指标 API 正常时）。

## 测试策略

- `internal/kube/metrics_test.go`：节点汇总、受限范围 Pod 汇总、节点指标 403 回退、指标 API 不存在、空指标、部分命名空间失败仍返回其余用量。
- `internal/api/overview_test.go`：`usage` 正常返回、不可用时 `unavailable` 含 `metrics`、受限用户不返回容量字段。
- 端到端：本地 k3s 对比 `kubectl top nodes` 与接口值（CPU 允许 ±50m、内存允许 ±10% 误差），并用受限账号确认为 `scope=pods`。
- 前端：`npm run lint`/`typecheck`，浏览器确认卡片、趋势与降级提示。

## Risks / Trade-offs

- **刷新后趋势丢失**：会话内趋势只适合「看一眼当前走势」，不是监控历史；已在卡片上标注，历史需求留给后续 Prometheus 集成。
- **metrics-server 采样间隔**：指标本身约 15 秒更新，30 秒轮询足够，不会造成明显锯齿。
- **受限用户的节点容量不可见**：只给绝对用量，无法算百分比；这是授权收敛的必然结果，页面上明确标注口径。
- **指标 API 冷启动**：空列表按零值处理，避免误报不可用；下一次轮询会恢复。