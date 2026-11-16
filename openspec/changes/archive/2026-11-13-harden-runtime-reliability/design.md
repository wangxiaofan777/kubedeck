# 设计：运行时可靠性收敛

## 1. 长连接超时

`rest.Config.Timeout` 会落到 `http.Client.Timeout`，覆盖读响应体的时间，因此长连接不能复用 10 秒总超时。

- `newDirectTransport` 构造三个配置：`reqCfg`（10 秒，用于普通 GET/POST/SSAR）、`streamCfg`（`Timeout=0`，用于 dynamic watch 与日志流）、`discCfg`（10 秒，用于 discovery 的 `ServerVersion`）。
- `DirectTransport` 的 `dynamic` 使用 `streamCfg`；`Watch` 的生命周期仅由请求 context（WebSocket 连接 context）决定。
- 日志的 `logRest` 已经是 `Timeout=0`，保持不变。

日志 follow 的 HTTP 侧：

- `Timeout` 中间件对 `GET .../log?follow=true` 跳过请求级超时。
- `streamLog` 开始时调用 `http.NewResponseController(w).SetWriteDeadline(time.Time{})`，解除 `http.Server.WriteTimeout` 对流的限制；连接关闭或流结束时随请求结束释放。

## 2. 审计导出与保留

- `QueryAudit` 不再使用交互分页的 `pageArgs`，改为显式上限 5000；交互列表在 `parseAuditFilter` 中限制 500。导出响应返回实际条数，UI 显示。
- `internal/app` 增加每日维护任务：`EnsureAuditPartitions` + `DropExpiredAuditPartitions(defaultRetentionDays)`。保留天数可配置，默认 180。
- 哈希链定义调整为「可截断前缀」：`VerifyAuditChain` 允许第一条存活记录的 `prev_hash` 不等于空串，返回 `truncated=true` 并继续校验后续链接；只有中间断裂才报告 `brokenId`。
- 为审计查询补索引：`(occurred_at)`、`(actor_subject, occurred_at)`、`(cluster_name, occurred_at)`、`(result, occurred_at)`。

## 3. 审计背压

`audit.Writer` 增加：

- `Available() bool`：队列占用低于 90% 且未处于降级重试耗尽状态。
- `Dropped() uint64`：累计丢弃数，导出到 Prometheus。
- 成功写入一批后清除 `degraded`。

`auditRequests` 中间件在写请求进入 handler 前检查 `Available()`；不可用时直接返回 503 `SERVICE_UNAVAILABLE`，错误信息说明审计不可用，写操作被拒绝。GET 请求不受影响。配置项 `KUBEDECK_AUDIT_STRICT`（默认 true）允许在极端情况下退化为继续（仅告警）。

退出流程：先关闭主队列，再在总 deadline 内刷写补偿队列；刷写失败只记录日志，不阻塞进程退出。

## 4. 命名空间单一来源

`requestNamespace` 改为：路径 `{namespace}` 存在时以其为准；query 中的 `namespace` 若存在且不一致，返回 400 `INVALID_ARGUMENT`。`transport()` 返回错误而不是静默选择其中一个。Pod 日志、驱逐、批量删除全部使用同一函数。

## 5. 输入与错误

- `parseKubeconfig` 拒绝 `auth.TokenFile`，与已拒绝的 `CAFile` 保持一致。
- `FromUpstream` 不再把 `err.Error()` 写入 `details.upstream`；只保留稳定 `code`、`upstreamReason` 与脱敏后的 message，原始错误写入服务端日志（带 requestId）。
- 集群探测消息对外只保留分类（不可达/凭据无效/证书失败），原始错误截断后写日志。

## 6. 平台角色约束

`bindingAllows` 增加动词维度：

- `viewer`：仅允许 `get/list/watch` 与 `can`。
- `developer`：允许读写，但集群级元数据修改仍需平台管理员。
- `cluster_admin`：允许读写与审计查看（审计仍由 `/admin` 路由的 `requireAdmin` 控制）。

`clusterAccess` 从请求方法推导动词（GET/HEAD 为读，其余为写），写请求对 `viewer` 返回 403。

## 7. 会话刷新

`requireIdentity` 在每次请求时用 `userID` 读取 `users` 表，刷新 `status` 与 `is_platform_admin`；用户被禁用时立即返回 401。组信息仍以登录时的 IdP claims 为准，并在会话 TTL 到期后重新登录刷新。

## 8. 实时订阅

- `resync` 通道容量从 8 提升到 `maxSubscriptionsPerConn * 2`，避免 20 个订阅同时失败时通知被丢弃。
- 重复订阅同一 ID 时先取消旧订阅再注册新订阅。
- 权限复核同时校验会话是否仍有效；会话失效时关闭连接。

## 9. 测试

- watch 保持 30 秒仍能收到事件（fake apiserver 延迟推送）。
- 日志 follow 持续 35 秒不被取消（httptest + 慢写）。
- 导出 500 条记录返回 500 条。
- 队列满时写请求返回 503，GET 正常。
- 路径与 query 命名空间不一致返回 400。
- 保留期删除后链校验返回 `truncated=true` 且不误报断裂。