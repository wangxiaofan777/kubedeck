# 权限矩阵

KubeDeck 有两层权限，二者都必须满足：

1. **平台角色**：决定用户能否看到某个集群/命名空间、能否管理平台用户与审计。
2. **目标集群 RBAC**：由 apiserver 对 Impersonate 后的用户/组做最终判定。平台角色不代替 Kubernetes 权限。

前端隐藏按钮只是体验优化；服务端与 apiserver 始终重新校验。

## 1. 平台角色

| 平台角色 | 集群管理 | 用户/绑定 | 审计 | 资源浏览 | 写操作入口 |
|---|---|---|---|---|---|
| PlatformAdmin | 登记/轮换/删除 | 管理全部 | 全部集群 | 受目标集群 RBAC 约束 | 受目标集群 RBAC 约束 |
| ClusterAdmin | 只读 | 不可管理 | 本集群 | 受目标集群 RBAC 约束 | 受目标集群 RBAC 约束 |
| Developer | 只读 | 不可管理 | 不可读 | 绑定范围内 | 绑定范围内，需 K8s 写权限 |
| Viewer | 只读 | 不可管理 | 不可读 | 绑定范围内只读 | 不展示；服务端仍会拒绝 |

平台角色是“可见范围”的门槛，不会自动授予 Kubernetes 权限。PlatformAdmin 如果自己的 K8s 身份没有权限，仍然只能看到 403。

### 命名空间可见性

- 只绑定具体命名空间的用户：`GET /clusters/{c}/namespaces` 直接返回绑定范围内的命名空间（去重排序），不要求集群级权限，也不会泄露范围外的名称。
- `*` 绑定或平台管理员：以用户身份向目标集群列举命名空间，只返回 apiserver 允许其查看的名称；该身份在目标集群需要 `namespaces` 的 `list` 权限。
- `GET /clusters/{c}/api-resources`、集群详情与健康信息只要求目标集群上存在任一绑定。
- 开发模式（`KUBEDECK_DEV_KUBECONFIG`）+ `KUBEDECK_DEV_PLATFORM_ADMIN=false` 可按数据库角色绑定运行，用于本地演示命名空间隔离；生产验证仍以 OIDC 普通账号为准。

## 2. 操作 × Kubernetes 权限

下表是控制台操作需要的最小 Kubernetes RBAC。`<resource>` 指目标资源；自定义资源同理。

| 操作 | API | Kubernetes 权限 |
|---|---|---|
| 资源发现 | `GET /api-resources` | `get` on discovery（通常所有认证用户可用） |
| 命名空间列表 | `GET /namespaces` | 平台绑定即返回绑定范围；`*` 绑定需要 `list` `namespaces` |
| 列表 | `GET /resources/...` | `list` `<resource>`（命名空间内） |
| 详情 | `GET /resources/.../{name}` | `get` `<resource>` |
| YAML 读取 | `GET /resources/.../{name}` | `get` `<resource>` |
| 创建 | `POST /resources/...` | `create` `<resource>`（`dryRun=All` 同样需要 `create`） |
| 部分更新 | `PATCH /resources/.../{name}` | `patch` `<resource>`；Server-Side Apply 冲突时由调用方显式请求 `force` |
| Diff / dryRun / 提交 | `POST .../diff`、`PUT` | `get` + `update`（dryRun 也可能需要 `create`） |
| 删除单个 | `DELETE .../{name}` | `delete` `<resource>` |
| 批量删除 | `DELETE /resources/...` | `deletecollection` 或逐个 `delete` |
| 伸缩 | `POST .../scale` | `get`/`update` `<resource>/scale`（Deployment 还需 `get` deployment） |
| 重启 | `POST .../restart` | `get` + `update` workload |
| 历史/回滚 | `GET .../history`、`POST .../rollback` | `get`/`list` ReplicaSet + `update` Deployment |
| 暂停/恢复 | `POST .../pause|resume` | `update` Deployment |
| 驱逐 Pod | `POST /pods/{ns}/{name}/evict` | `create` `pods/eviction`；受 PDB 约束 |
| 日志 | `GET /pods/{ns}/{name}/log` | `get` `pods/log` |
| 事件 | `GET /events` | `list` `events` |
| 实时订阅 | `GET /stream` | `list` + `watch` `<resource>` |
| 权限预检 | `POST /auth/can` | `create` `selfsubjectaccessreviews`（默认允许） |
| 审计查询/导出 | `/admin/audit-logs*` | 平台权限，不访问目标集群 |

## 3. 推荐 Kubernetes ClusterRole

Viewer（绑定到命名空间 RoleBinding）：

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubedeck-viewer
rules:
  - apiGroups: [""]
    resources: ["pods", "services", "configmaps", "events", "namespaces", "nodes", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets", "replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods"]
    verbs: ["get", "list"]
```

总览的资源用量同样走 Impersonation：节点级用量与容量需要 `nodes.metrics.k8s.io` 的 `list` 权限（通常来自集群级绑定）；仅绑定到命名空间的用户只会拿到这些命名空间内 Pod 的用量合计，响应中不会出现节点容量或使用率。

Developer 在此基础上增加写权限（按命名空间绑定）：

```yaml
rules:
  - apiGroups: [""]
    resources: ["pods", "services", "configmaps"]
    verbs: ["create", "update", "patch", "delete", "deletecollection"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["create", "update", "patch", "delete"]
  - apiGroups: ["apps"]
    resources: ["deployments/scale", "statefulsets/scale", "replicasets/scale"]
    verbs: ["get", "update", "patch"]
  - apiGroups: [""]
    resources: ["pods/eviction"]
    verbs: ["create"]
```

不要把 `cluster-admin` 绑定给普通用户；Secrets 的读取权限应按最小范围单独授予，因为 Secret 明文会通过 YAML 视图暴露。

## 4. 平台管理 API

| 接口 | 所需平台角色 |
|---|---|
| `POST /clusters`、`PUT /clusters/{c}/credential`、`DELETE /clusters/{c}` | PlatformAdmin |
| `GET /admin/users`、`POST /admin/users/{id}/status` | PlatformAdmin |
| `PUT /admin/role-bindings`、`DELETE /admin/role-bindings/{id}` | PlatformAdmin |
| `GET /admin/audit-logs`、`verify`、`export` | PlatformAdmin |
| `GET /admin/roles` | PlatformAdmin |
| `GET /clusters`、资源浏览 | 有平台绑定的用户 |

## 5. 与规格的对应

- 身份透传：以上所有资源操作都由 Impersonation 发送，apiserver 用最终用户 RBAC 判定。
- 权限预检：SSAR 结果最多缓存 30 秒，小于规格的 60 秒上限。
- 回收权限：平台绑定变更后访问缓存失效，WebSocket 权限复核每 30 秒执行，订阅在 60 秒内终止。
- 初始管理员：bootstrap 只创建一次，成功后返回 409。
- 只读用户：前端用 `<Can>` 隐藏写入口，服务端与 apiserver 仍会拒绝绕过请求（已验证 `delete` 返回 403）。