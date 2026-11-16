## Context

见 `proposal.md — Why`。相关现状：

- `useResourceRealtime` 只发送 `group/version/resource/namespace/resourceVersion`；服务端 `watchLoop` 在 watch 关闭时发送 `resync` 后 `return`，并用 defer 删除订阅。客户端收到 `resync` 只调用 `invalidateQueries`，没有重新 `subscribe`。
- `useCan` 把 `scale`/`restart`/`evict` 原样放进 SSAR；后端 `reviewAccess` 原样透传 `Verb`。
- `Timeout(30s)` 只豁免 WebSocket 与 `GET /log?follow=true`；`POST /nodes/{name}/drain` 使用 `DrainNode` 的 120 秒默认超时，但请求上下文在 30 秒被取消。
- `StatusPill` 的规则数组先匹配成功关键词，再匹配危险关键词。
- 集群接入/删除没有 invalidate `['clusters']`；`switchCluster` 保留原 path 后缀与 query。
- `TestMigrateIsIdempotent` 硬编码 3 个迁移，实际已有 4 个。

## Goals / Non-Goals

**Goals:**

- 实时推送在一次 resync 后自动恢复，并尊重列表过滤条件。
- 预检动词与 Kubernetes RBAC 语义一致，合法用户能看到可用操作。
- 驱逐请求按自身超时执行。
- 状态颜色与集群上下文不再给出错误信息。
- 数据库测试与 CI 闸门恢复可信。

**Non-Goals:**

- 不把实时层改为共享 informer 或服务端多副本广播。
- 不把驱逐改成异步 operation（保留为后续容量优化）。
- 不重构整个权限模型；只修正动作到 RBAC 动词的映射。
- 不引入新的前端状态库。

## Decisions

### 1. 实时客户端在 `resync` 后重建订阅

把发送订阅的逻辑提取为 `sendSubscribe(resourceVersion)`；`onopen` 与 `resync` 共用。收到 `resync` 时：

1. `invalidateQueries` 重新拉取列表；
2. 清空 `rvRef`；
3. 若 socket 仍为 `OPEN`，发送新的 `subscribe`，`resourceVersion` 为空字符串。

增加连续 resync 保护：5 秒内收到第二次 `resync` 时关闭 socket，交给现有指数退避重连，避免服务端持续拒绝旧版本时形成忙循环。

订阅参数增加 `labelSelector`，并加入 effect 依赖；订阅 ID 仍使用 GVR/namespace，不把 selector 拼进 ID，避免切换过滤条件时产生重复订阅（effect 会先清理旧连接）。

替代方案：服务端在 resync 后保留订阅并等待客户端续传。需要服务端维护“半开订阅”状态，复杂度更高；当前服务端已明确移除订阅，客户端重建更简单。

### 2. 权限预检使用真实动词与子资源

`useCan` 增加 `subresource?: string`，并透传到 `api.can`。工作负载动作按下表映射：

| 平台动作 | verb | subresource |
|---|---|---|
| 伸缩 | `update` | `scale` |
| 重启 | `patch` | 无 |
| 历史/回滚 | `patch` | 无 |
| 暂停/恢复 | `patch` | 无 |
| Pod 驱逐 | `create` | `eviction` |

映射集中在一个纯函数 `workloadCanCheck(action, gvr)` 中，组件只引用结果，避免各处手写。预检仍只是界面提示；实际写请求继续由平台准入与 apiserver 判定。

替代方案：后端新增 `/auth/can-action` 把平台动作翻译为 RBAC 检查。会引入第二套动作语义与额外接口，本期不采用。

### 3. 驱逐请求豁免普通请求超时

在 `isLongLivedRequest` 中识别 `POST` 且路径以 `/drain` 结尾的请求，跳过 30 秒 `Timeout` 中间件；`DrainNode` 自身的 120/300 秒上下文仍生效。响应仍是同步 JSON，前端现有 loading 与逐项结果展示不变。

替代方案：改成 `POST /drain` 返回 operationId，前端轮询 `/operations/{id}`。语义更好，但需要新的 operation 类型、持久化与前端进度 UI，超出本次修复范围。

### 4. 状态颜色先判危险，再判进行中，最后判成功

调整 `RULES` 顺序为 danger → warning → success → info，并使用词边界或明确的否定优先规则，确保 `NotReady` 不会命中 `ready`。为 `statusTone` 增加单元测试，覆盖 `NotReady`、`CrashLoopBackOff`、`Running`、`Ready`、`Unknown`。

### 5. 集群上下文以“新集群总览”为默认落点

- 集群接入成功后：`invalidateQueries(['clusters'])`，再导航到设置页。
- 集群删除成功后：`invalidateQueries(['clusters'])`，再导航到 `/clusters`。
- `switchCluster`：固定导航到 `/clusters/<new>/overview`，不保留 path 后缀与 query。若用户希望保留列表类型，可在总览页重新选择；这比跳到不存在的对象更安全。

### 6. 迁移测试从嵌入文件推导期望

`TestMigrateIsIdempotent` 不再硬编码迁移列表，而是从 `migrationsFS` 的 `migrations/*.sql` 推导版本名并与 `AppliedMigrations` 比较；仍然断言无重复、checksum 全部非空。这样新增迁移不会再次让测试失真。

## Risks / Trade-offs

- [客户端在 resync 后立即重订阅可能重复收到事件] → 重订阅使用空 RV，服务端从当前状态开始 watch；列表已 invalidate，重复事件按 UID 合并，不会产生重复行。
- [连续 resync 保护可能让用户感知为断线] → 关闭连接后走既有退避重连，最多延迟数秒；比永久静默停止更好。
- [预检映射与后端实际动作不一致] → 映射集中在纯函数并加测试；后端动作实现变更时测试会提醒。
- [驱逐长请求占用连接与 goroutine] → 仅对 `/drain` 豁免，且受 300 秒上限约束；后续可迁移到 operation。
- [切换集群总是回总览，用户可能觉得少了一步] → 避免错误上下文优先；总览本身提供到各资源的入口。

## Migration Plan

1. 前端实时与预检修复可独立发布；后端 drain 超时豁免需与前端同版本发布。
2. 迁移测试修正只影响测试，不改变运行时行为。
3. 回滚：各修复均为局部改动，可按文件回滚；无数据库迁移。

## Open Questions

- 是否需要为实时 resync 增加指标（如 `kubedeck_realtime_resync_total`）？本期先不加，等有实际观测需求再补。