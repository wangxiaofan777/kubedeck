## 1. 后端聚合逻辑

- [x] 1.1 在 `internal/kube/overview.go` 定义总览数据结构（scope、nodes、namespaces、pods、workloads、events、unavailable、fetchedAt），并搭好基于 `Transport` 的采集骨架；验证：`go build ./...` 通过
- [x] 1.2 实现节点采集：统计总数与 Ready 数，汇总 `status.allocatable` 的 CPU（毫核）与内存（字节），解析失败的节点跳过；验证：用 fake 节点对象编写单测，覆盖 1 个 NotReady、1 个 allocatable 解析异常
- [x] 1.3 实现 Pod 状态分布与 `crashLoop` 计数：按 phase 分类，统计 `CrashLoopBackOff`/`ImagePullBackOff` 容器；验证：单测覆盖 Pending/Failed/Running+CrashLoopBackOff 与重复计数边界
- [x] 1.4 实现工作负载异常计数：Deployment/StatefulSet 比较就绪副本与期望副本，DaemonSet 比较 `numberReady` 与 `desiredNumberScheduled`；验证：单测覆盖副本为 0、未设置 `spec.replicas`、部分就绪三种情况
- [x] 1.5 实现事件摘要：按 `lastTimestamp`（回退 `eventTime`/`creationTimestamp`）倒序取前 20 条，`message` 截断 200 字符；验证：单测覆盖排序、时间字段回退与截断
- [x] 1.6 实现权限收敛与回退：先尝试全命名空间 list，遇 `Forbidden` 改为按 `Namespaces()` 返回的命名空间逐个 list，并发上限 8，单个命名空间 `Forbidden` 跳过；验证：单测用 fake `Transport` 断言调用序列与并发上限，并确认无权限命名空间不计入任何计数
- [x] 1.7 实现 5000 对象上限与 `continue` 分页，达到上限置 `scope.truncated=true`；验证：单测构造超过上限的列表，断言计数封顶且标志为真
- [x] 1.8 实现部分失败汇总：单项超时/5xx 记入 `unavailable` 并继续采集其它项；验证：单测覆盖「事件失败但节点与 Pod 成功」与「全部失败」两种结果

## 2. HTTP 接口

- [x] 2.1 新增 `internal/api/overview.go` 处理器：校验登录、集群可见性与存在性，调用聚合逻辑并序列化；验证：`go test ./internal/api -run Overview` 覆盖 200、集群不存在 404、不可见 403 三种路径
- [x] 2.2 按 design D6 映射错误码：集群不可达返回 `CLUSTER_UNREACHABLE`，全部统计项失败返回 `CLUSTER_DEGRADED`；验证：处理器单测断言错误码与 HTTP 状态码
- [x] 2.3 在 `internal/api/router.go` 注册 `GET /api/v1/clusters/{cluster}/overview`；验证：`go test ./internal/api -run Routes` 或路由一致性测试通过
- [x] 2.4 在 `docs/openapi.yaml` 补齐该操作与响应 schema；验证：`go test ./internal/api -run OpenAPI` 通过，且路由一致性测试未报缺失

## 3. 前端总览页

- [x] 3.1 在 `web/src/lib/api.ts` 增加总览响应类型与请求函数；验证：`npm run typecheck` 通过
- [x] 3.2 新增 `web/src/features/overview/useOverview.ts`：加载、手动刷新、30 秒自动刷新、页面不可见时暂停；验证：类型检查通过，并在浏览器 Network 面板确认切到后台后不再轮询
- [x] 3.3 新增 `OverviewPage`：用 antd `Card`/`Statistic`/`Progress`/`Table` 展示节点、容量、Pod 状态、工作负载与事件，区分加载中、空数据、失败、降级四态，并显示数据生成时间；验证：`npm run lint && npm run typecheck` 通过，且本地页面在构造失败响应时展示降级提示
- [x] 3.4 在 `web/src/App.tsx` 增加 `/clusters/:cluster/overview` 路由，并把 `HomeRedirect` 改为跳转第一个集群的总览；验证：本地启动后访问 `/` 落在总览页，旧的 `/clusters/{cluster}/pods` 链接仍可访问
- [x] 3.5 在 `web/src/app/ConsoleShell.tsx` 增加「总览」菜单项并纳入选中态；验证：切换集群与资源类型时菜单高亮正确

## 4. 验证与验收

- [x] 4.1 后端全量测试：`make test` 通过，新增单测覆盖 D3 权限回退与 D4 异常口径
- [x] 4.2 提交前统一检查：`make check` 通过（后端 lint/测试 + 前端 lint/typecheck）
- [x] 4.3 在本地 k3s 上用管理员账号核对总览计数与 `kubectl get` 结果一致，并确认请求未写入审计记录；验证：页面数字与命令输出一致，审计查询无新增 overview 记录
- [x] 4.4 用仅有单个命名空间权限的账号验收：总览只统计该命名空间，其他命名空间的对象既不计数也不出现在事件列表中；验证：对比管理员视图与受限账号视图的计数差异仅限授权范围
- [x] 4.5 验证降级路径：停掉 k3s 后请求总览返回 `CLUSTER_UNREACHABLE` 且页面展示重试入口；恢复后刷新可正常加载