# 设计：节点管理

## 1. 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/clusters/{c}/nodes` | 复用通用列表，前端专用列渲染 |
| GET | `/clusters/{c}/nodes/{name}` | 复用通用详情 |
| GET | `/clusters/{c}/nodes/{name}/pods` | 按 `spec.nodeName` 过滤的 Pod 列表 |
| POST | `/clusters/{c}/nodes/{name}/cordon` | 标记不可调度 |
| POST | `/clusters/{c}/nodes/{name}/uncordon` | 恢复调度 |
| POST | `/clusters/{c}/nodes/{name}/drain` | 驱逐节点上的 Pod |

所有路由要求集群级绑定（`*`）或平台管理员；apiserver 仍以用户身份判定 `nodes/patch`、`pods/eviction` 权限。

## 2. 请求与响应

`POST /drain`：

```json
{
  "cordon": true,
  "force": false,
  "ignoreDaemonSets": true,
  "deleteEmptyDirData": false,
  "gracePeriodSeconds": 30,
  "timeoutSeconds": 120
}
```

响应：

```json
{
  "node": "worker-1",
  "cordoned": true,
  "evicted": 12,
  "skipped": 2,
  "failed": 1,
  "results": [
    {"namespace": "prod", "name": "web-1", "result": "evicted"},
    {"namespace": "prod", "name": "db-0", "result": "blocked", "code": "PDB", "message": "..."}
  ]
}
```

- `blocked` 不会中断其他 Pod；整体返回 200，由前端展示逐项结果。
- 若一个 Pod 都无法驱逐且全部被 PDB 阻止，返回 409 `CONFLICT` 并附结果。

## 3. Drain 算法

1. 读取节点，确认存在；如 `cordon=true` 且节点可调度，先 patch `spec.unschedulable=true`。
2. 列出 `spec.nodeName=<node>` 的 Pod（所有命名空间，分页取完）。
3. 过滤：
   - `metadata.ownerReferences` 含 DaemonSet 且 `ignoreDaemonSets=true` → skip。
   - 镜像 Pod（annotation `kubernetes.io/config.mirror`）→ skip。
   - 已终止（`status.phase` 为 Succeeded/Failed）→ skip。
   - 无控制器 owner 且 `force=false` → blocked（`UNMANAGED`）。
   - 使用 emptyDir 且 `deleteEmptyDirData=false` → blocked（`LOCAL_STORAGE`）。
4. 对剩余 Pod 逐个 `Evict`，带 `gracePeriodSeconds` 与 `timeoutSeconds`。
5. 单个失败不中断；记录 `PDB` / `FORBIDDEN` / `NOT_FOUND` / `OTHER`。
6. 结果写入审计 `verb=node.drain`，detail 只保存计数与节点名，不保存 Pod 内容。

## 4. 并发与超时

- 默认串行驱逐，避免同时触发大量 PDB 拒绝；`timeoutSeconds` 是整体预算，超时后停止并返回已完成结果。
- 请求级 context 30 秒不足；drain 路由使用独立 120 秒 context（可通过请求体覆盖，上限 300 秒）。

## 5. 前端

- 节点列表使用专用表格组件，而不是通用资源表：
  - 状态用 `StatusPill`；Roles 从 `node-role.kubernetes.io/*` 标签推导。
  - CPU/内存列优先使用 metrics API，缺失时显示可分配值。
  - 不可调度节点显示「已 cordon」标签。
- 节点详情：
  - 顶部：状态、版本、内部/外部 IP、Age、操作按钮。
  - 卡片：Conditions、Capacity/Allocatable、Taints、Labels。
  - Pod 表：命名空间、名称、状态、重启、Age；点击进入 Pod 详情。
- 操作：
  - cordon/uncordon 为即时操作 + 轻量确认。
  - drain 打开 Drawer：列出将被驱逐的 Pod、将被跳过的 Pod、被阻止的 Pod（含原因）、选项、整体超时。
  - 执行中展示进度条与逐项结果；失败项可复制。

## 6. 审计

`auditTarget` 增加：

- `/nodes/{name}/cordon` → `node.cordon`
- `/nodes/{name}/uncordon` → `node.uncordon`
- `/nodes/{name}/drain` → `node.drain`

资源名记为 `nodes`，对象名为节点名。