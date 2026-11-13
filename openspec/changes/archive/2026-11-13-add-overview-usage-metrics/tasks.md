## 1. 后端：用量采集

- [x] 1.1 新增 `internal/kube/metrics.go`：定义节点指标与 Pod 指标 GVR、`Usage` 结构与 `CollectUsage`，节点路径汇总 CPU/内存并复用总览已读取的可分配容量；验证：单元测试覆盖节点汇总与容量百分比计算（两位小数）。
- [x] 1.2 实现范围受限路径：按 `OverviewOptions.Namespaces` 逐个命名空间读取 Pod 指标并汇总，复用现有并发信号量与总超时；验证：单元测试覆盖多个命名空间的合计与部分命名空间 403 时保留其余结果。
- [x] 1.3 实现节点指标 403 时的回退：受限或拒绝节点指标时改走 Pod 指标，不因节点权限缺失导致整体不可用；验证：单元测试覆盖「节点指标 403 → Pod 路径成功」。
- [x] 1.4 处理不可用与空样本：指标 API 不存在、读取失败、超时统一返回 `ErrMetricsUnavailable`；指标列表为空返回零值且 `Available=true`；验证：单元测试覆盖 404、超时、空列表三类分支。
- [x] 1.5 在 `Overview` 增加 `Usage *OverviewUsage` 并在 `CollectOverview` 中与既有统计并行采集；任一指标失败只把 `metrics` 加入 `unavailable`，不返回错误；验证：`internal/kube/overview_test.go` 新增「指标失败但总览成功」用例（`TestOverviewDegradesMetricsFailure`）。

## 2. 后端：接口与契约

- [x] 2.1 `internal/api/overview.go` 直接序列化新增 `usage` 字段，确保 `usage=null` 与 `unavailable` 含 `metrics` 同时出现；验证：`TestClusterOverviewUsageFromNodeMetrics`、`TestClusterOverviewMetricsUnavailable`。
- [x] 2.2 受限用户响应中不出现容量与百分比字段；验证：`TestClusterOverviewScopedUsageHidesCapacity` 断言 `scope=pods` 且容量字段为 `null`。
- [x] 2.3 `docs/openapi.yaml` 增加 `ClusterUsage` schema、overview 响应 `usage` 字段与 `unavailable` 的 `metrics` 取值；验证：YAML 解析通过（`ClusterUsage` 可解析），字段与实现一致。
- [x] 2.4 `docs/permission-matrix.md` 补充说明：指标读取沿用 Impersonation，节点级用量需要节点指标读权限，受限用户只统计可见命名空间 Pod；验证：文档审阅，无新增授权路径。

## 3. 前端：用量卡片与趋势

- [x] 3.1 `web/src/lib/api.ts` 增加 `ClusterUsage` 类型与 overview 响应字段，兼容 `usage` 为 `null`；验证：`npm run typecheck` 通过。
- [x] 3.2 `useOverview` 在成功响应后累积会话内采样（最多 20 点，30 秒一点），切换集群或组件卸载时清空；验证：浏览器中刷新后趋势重置，同一时间点不重复追加。
- [x] 3.3 `OverviewPage` 增加「资源用量」卡片：当前 CPU/内存、使用率进度条、内联 SVG 趋势线；验证：浏览器中显示 0.07 核 / 3.30%，连续两次取数后出现 2 条折线。
- [x] 3.4 降级与受限展示：`usage=null` 显示「指标不可用」提示；`scope=pods` 隐藏容量与百分比并标注「仅统计可见命名空间」；验证：受限账号页面显示 0.99 核且无进度条；无指标权限时接口 `usage=null`、`unavailable=[metrics]`。
- [x] 3.5 快速刷新/切换集群时不重复追加同一采样点，避免趋势折线出现伪波动；验证：`useUsageTrend` 以 `fetchedAt` 去重，浏览器连续刷新仅新增一个点。

## 4. 验证

- [x] 4.1 本地 k3s 端到端：总览接口的 CPU/内存与 `kubectl top nodes` 对比。实测：接口 84m / 2715MiB（4.2% / 45.79%），`kubectl top nodes` 84m / 2714MiB（4% / 45%）；压测探针运行时接口 1064m，`kubectl top` 1074m，误差在预算内。
- [x] 4.2 受限账号验证：alice（平台绑 default/dev，k3s 仅 default 存在）请求总览，返回 `scope=pods`、`cpuAllocatableMilli=null`、`cpuPercent=null`，Pod 探针运行时用量 981m，与 `kubectl top pod` 一致；页面标注「仅可见命名空间」。为复现需给命名空间角色补充 `metrics.k8s.io/pods` 读权限，已同步补进 `kubedeck-view-pods` 测试角色。
- [x] 4.3 降级验证：alice 在没有指标权限时请求总览，返回 200、`usage=null`、`unavailable=["metrics","nodes"]`，页面其余统计正常；删除测试探针后恢复为节点口径。
- [x] 4.4 确认后端 `gofmt`/`go vet`/`staticcheck`/`go test ./cmd/... ./internal/...` 与前端 `lint`/`typecheck` 全部通过；总览读取后 `audit_logs` 最近记录只有 `login`，没有总览/资源读取审计，数据库未新增用量写入。
- [x] 4.5 更新任务勾选，`openspec validate add-overview-usage-metrics --strict` 通过后归档并提交。

## 5. 收尾

- [x] 5.1 在浏览器完成一次总览页人工验收：数值、趋势（2 个采样点、30 秒标签）、受限口径与不可用提示均符合预期。
- [ ] 5.2 归档变更、同步主规格、提交并推送。