## Context

见 `proposal.md` 的 Why。当前实现状态中有几个约束直接决定做法：

- 所有集群读取都通过按用户构造的 `Transport` 直连 apiserver（Impersonation），**没有资源 informer 缓存**，只有 5 分钟 TTL 的资源发现缓存（`internal/kube/resources.go`）。总览只能按需 list。
- `Transport` 接口是平台与集群访问的唯一抽象（`internal/kube/transport.go`），未来 Agent 模式还要实现它，接口不宜因统计而膨胀。
- `ClusterInfo` 只有名称、版本与健康状态；设计文档第 571 行要求的「资源统计」从未实现。
- 前端没有图表依赖，`k8s-console-design.md` 选型中的 ECharts 属于 Phase 2。
- 审计只记录写操作与敏感读，总览属于只读浏览，不应产生审计记录。

## Goals / Non-Goals

**Goals:**

- 用一次请求拿到单集群总览，页面首屏只打一个接口。
- 统计严格按当前用户授权范围收敛，跨用户不共享聚合结果。
- 单项数据不可用时返回部分结果并显式标注，不拖垮整页。
- 不新增存储、不新增依赖、不改动写路径与授权模型。

**Non-Goals:**

- 不做审计统计聚合（P1）、不做 CPU/内存使用率趋势与 Prometheus 代理（P2）。
- 不引入服务端缓存层，不做跨用户共享的统计缓存。
- 不把总览计数用于任何授权判断或自动化决策。
- 不实现历史快照、告警或容量预测。

## Decisions

### D1：新增独立聚合接口，而不是扩展 `/clusters`

`GET /api/v1/clusters/{cluster}/overview` 独立于集群列表。扩展 `/clusters` 会让列表接口在用户有 N 个集群时产生 N 倍扇出，且登录前就需要聚合数据；独立接口按需拉取、按集群失败隔离。

响应结构（字段名以 `docs/openapi.yaml` 为准）：

```json
{
  "cluster": "prod",
  "fetchedAt": "2026-11-13T10:00:00Z",
  "scope": { "allNamespaces": true, "namespaces": 12, "truncated": false },
  "nodes": { "total": 5, "ready": 4, "allocatable": { "cpuMilli": 16000, "memoryBytes": 68719476736 } },
  "namespaces": { "total": 12 },
  "pods": { "total": 120, "running": 110, "pending": 3, "succeeded": 5, "failed": 2, "unknown": 0, "crashLoop": 1 },
  "workloads": {
    "deployments": { "total": 20, "unavailable": 2 },
    "statefulSets": { "total": 4, "unavailable": 0 },
    "daemonSets": { "total": 6, "unavailable": 1 }
  },
  "events": [
    { "type": "Warning", "reason": "BackOff", "namespace": "default", "object": "Pod/web-1", "message": "…", "lastTimestamp": "…" }
  ],
  "unavailable": ["events"]
}
```

`scope.allNamespaces=false` 表示统计只覆盖逐个读取成功的命名空间；`truncated=true` 表示某类对象超过采集上限。`unavailable` 为空数组表示全部统计项可用。

### D2：聚合逻辑放在 kube 包，依赖 `Transport` 而不扩展它

新增 `internal/kube/overview.go`，函数签名接收 `Discovery` 结果与 `Transport`，用现有的 `List`/`Namespaces` 完成采集。这样：

- `Transport` 接口保持不变，Agent 实现不需要被迫提供统计能力；
- 可以用 fake `Transport` 做单元测试，覆盖 403、分页、截断等分支；
- HTTP 层只负责鉴权、集群可见性检查与序列化。

备选方案是给 `Transport` 加 `Overview` 方法：会让每个传输实现都承担统计逻辑，且未来 Agent 侧要做同样聚合，复用性更差。

### D3：权限收敛采用「先尝试跨命名空间，失败再逐命名空间」

采集顺序：

1. `Namespaces(ctx)` 拿到用户可见命名空间（apiserver 已按用户权限过滤）。
2. 对每类资源先尝试 `List(namespace="")`（全命名空间）。
3. 若返回 `Forbidden`：改为对第 1 步的每个命名空间逐个 `List`，并发上限 8；单个命名空间 `Forbidden` 直接跳过，不视为错误。
4. 其它错误（超时、5xx）：把该资源类型记入 `unavailable`，继续采集其它类型。

对非管理员，第 1 步的命名空间列表直接复用平台已有的 RoleBinding 范围，不再要求用户额外具备 namespace 列表权限（与 `/namespaces` 接口的既有行为一致）；拥有 `*` 绑定或平台管理员的用户仍由 apiserver 枚举可见命名空间。

不使用 `SelfSubjectAccessReview` 预检：它只能判断「某资源在某命名空间」，无法枚举可见命名空间集合，预检成本高于直接尝试；而且预检结果与实际 list 之间仍可能不一致。

采集上限：每类资源最多 5000 个对象，用 `Limit` + `continue` 分页；达到上限即停止并置 `scope.truncated=true`。上限是为了让超大集群的总览仍然在可接受时间内返回，而不是精确统计一切。

### D4：容量与异常口径

- 节点就绪：`Node.status.conditions` 中 `Ready=True`。
- 容量：汇总 `status.allocatable` 的 CPU 与内存，CPU 归一化为毫核（`cpuMilli`），内存归一化为字节（`memoryBytes`）；解析失败时跳过该节点并记入日志，不使整个请求失败。
- Pod 异常：`phase` 不在 `Running`/`Succeeded`，或容器 `waiting.reason` 为 `CrashLoopBackOff`/`ImagePullBackOff`。崩溃容器计入 `crashLoop`，同时该 Pod 仍按其 phase 计入对应分类，两个口径互不覆盖。
- 工作负载异常：Deployment/StatefulSet 比较 `status.readyReplicas` 与 `spec.replicas`（未设置按 1）；DaemonSet 比较 `status.numberReady` 与 `status.desiredNumberScheduled`。

### D5：事件摘要

最近事件取 `lastTimestamp`（为空时用 `eventTime` 或 `metadata.creationTimestamp`）倒序前 20 条，`message` 截断到 200 字符。事件读取失败只影响 `events` 一项；`unavailable` 含 `events` 时前端在该区域显示降级提示。

### D6：HTTP 语义与错误码

- 集群不存在 → `404 NOT_FOUND`；用户对该集群不可见 → `403 FORBIDDEN`（与既有集群接口一致，避免泄露集群存在性）。
- 集群健康状态为不可达 → `503 CLUSTER_UNREACHABLE`（沿用既有错误码映射），由前端展示重试入口。
- 至少一个统计项成功 → `200`，失败项进 `unavailable`；全部统计项失败且集群可连 → `503 CLUSTER_DEGRADED`（沿用既有错误码映射）。
- 总览不计入操作审计；现有访问日志与 Prometheus 指标自动覆盖。

### D7：前端不引入图表库

P0 用 antd 的 `Card`、`Statistic`、`Progress`、`Table` 组合展示，不新增依赖。页面默认 30 秒自动刷新、页面不可见时暂停，并提供手动刷新与「数据生成于」时间。P2 引入 ECharts 时再替换展示层。

默认落地页从「第一个集群的第一个资源列表」改为「第一个集群的总览」；旧链接 `/clusters/{cluster}/pods` 继续可用，不做重定向。

### D8：不做服务端缓存

聚合结果与用户身份强相关，缓存键必须包含用户与权限范围，容易因权限变更产生陈旧数据；当前页面刷新频率低（30 秒），直接实时读取更简单、更安全。若后续观测到 apiserver 压力，再引入按用户短 TTL 缓存。

## Risks / Trade-offs

- [大集群聚合慢] → 每类资源 5 秒超时、整体 8 秒上限、5000 对象截断；超时项进 `unavailable`，页面降级展示并允许重试。
- [逐命名空间回退产生请求风暴] → 仅在全命名空间 list 被拒时触发；并发上限 8；单个命名空间失败即跳过。
- [计数被误当作授权依据] → 规格明确总览不用于授权；所有对象入口点击后仍由服务端重新校验。
- [事件消息可能含敏感信息] → 只读取用户有权访问的命名空间；消息截断到 200 字符；不展示完整对象内容。
- [权限变更后统计与实际访问不一致] → 不做服务端缓存，每次实时读取，降低陈旧窗口。
- [总览接口增加 apiserver 负载] → 前端低频刷新、页面不可见时暂停；后续可按用户加短 TTL 缓存。

## Migration Plan

无数据迁移。部署后新页面立即可用；回滚只需移除路由与前端入口，统计不产生任何持久化副作用。