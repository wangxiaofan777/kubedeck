## Context

`listNamespaces` 先调用 `h.transport(w, r)`，该调用用 `clusterAccess(cluster, namespace)` 做准入；命名空间列表请求没有 namespace 参数，因此等价于"集群级"请求，只接受 `*` 绑定。结果是：只绑定具体命名空间的用户在第 130 行就被 403 拒绝，第 135-156 行的"按绑定过滤"逻辑对这类用户永远不可达。`apiResources`、`getCluster`、`getClusterHealth` 复用同一准入方式，也存在同样问题。

前端的命名空间选择器无条件加入"全部命名空间"选项；首页跳转到 `/clusters/{c}/pods` 时不带 namespace，于是命名空间级用户首次进入就会发起跨全部命名空间的列表请求并得到 403。

平台绑定通过 `domain.RoleBindingInput.Namespace` 表达范围，空值在写入时规范化为 `*`；`bindingsFor` 每次请求读库，绑定变更即时生效（`/auth/can` 另有 30 秒缓存）。

## Goals / Non-Goals

**Goals:**

- 让"集群可见"与"命名空间可访问"成为两个独立判定：有任一绑定的用户可以使用集群级只读信息与资源发现；只有跨命名空间操作才要求 `*`。
- 命名空间列表严格落在用户的绑定范围内，不泄露范围外的名称。
- 前端不再诱导用户进入必然 403 的"全部命名空间"视图。

**Non-Goals:**

- 不改变平台角色（PlatformAdmin / ClusterAdmin / Developer / Viewer）的语义，也不新增权限表。
- 不对每个命名空间执行额外的 `SelfSubjectAccessReview` 探测；`*` 绑定使用 impersonated list 的结果。
- 不新增权限管理界面；绑定仍通过现有管理 API 维护。
- 不改变开发模式固定为 PlatformAdmin 的行为。
- 不调整已发布的资源列表/详情接口的命名空间准入语义。

## Decisions

### D1：新增 `clusterVisible`，与 `clusterAccess` 分离

```go
func (h *handlers) clusterVisible(ctx, id, cluster string) (bool, error)
```

- 平台管理员直接允许；
- 非管理员：存在任意 `ClusterName == cluster` 的绑定即允许；
- 错误处理与 `clusterAccess` 一致（存储错误向上映射）。

`clusterAccess(cluster, namespace)` 继续用于"某命名空间内能否操作"的判断，语义不变：namespace 为空时仍只接受 `*`，因为空命名空间代表跨全部命名空间的列表或集群级资源。

替代方案是让 `clusterAccess` 对空 namespace 放宽为"任一绑定"，但这会让命名空间级用户能够跨命名空间列资源（例如 `?namespace=` 省略时），属于越权，不能接受。

### D2：`listNamespaces` 按绑定分支，不再复用 `transport`

新流程：

1. 从注册表取出集群；不存在返回 404。
2. `clusterVisible` 判定；不通过返回 403。
3. 平台管理员：`TransportFor` 后 `Namespaces()`，返回全部（受 apiserver 约束）。
4. 非管理员读取绑定：
   - 存在 `*`：`TransportFor` 后 `Namespaces()`，返回以用户身份可见的命名空间；
   - 只有具体命名空间：返回绑定中的命名空间，去重并按字典序排序，不访问 apiserver；
   - 没有该集群绑定：403（与第 2 步一致）。
5. 需要访问 apiserver 的分支保留集群不可达检查（复用 `transportFor` 的错误映射或显式 `Info` 判断），返回 503。

具体命名空间分支不调用 `transportFor`，因为该函数会对空 namespace 做跨命名空间准入；改为直接 `cluster.TransportFor(id.UserRef())`，仅在 `*`/管理员分支使用。

### D3：`*` 绑定仍以用户身份枚举命名空间

不使用平台 ServiceAccount 列出全部命名空间。`*` 表示平台可见范围是全部，但命名空间名称本身仍是集群信息，应由 apiserver 对 impersonated 身份判定。若用户在目标集群没有 `list namespaces` 权限，接口返回上游 403；权限矩阵中的推荐 ClusterRole 已包含 `namespaces` 的 `get/list/watch`，文档补充说明。

替代方案是对 `*` 绑定直接用平台身份列出全部名称，能保证下拉框永远有值，但违背"授权只走 Impersonation"的架构约束，不采用。

### D4：前端以 `me.bindings` 推导集群级范围

- `clusterWide = me.user.isPlatformAdmin || me.bindings.some(b => b.clusterName === cluster.name && b.namespace === '*')`。
- 命名空间选项：`clusterWide` 时包含"全部命名空间"；否则只渲染接口返回的列表。
- 首次进入且 `type.namespaced && !namespace && !clusterWide`：用 `replace` 导航到同一路径并带上第一个可访问命名空间，避免先发一次必然 403 的请求。只在 namespace 为空时自动选择，用户显式输入的越权命名空间仍交给资源页展示"无访问权限"。
- 导航菜单过滤：`!clusterWide` 时隐藏 `namespaced === false` 的资源类型（Namespaces、Nodes），因为平台准入必然拒绝；直接输入 URL 仍由服务端返回 403。
- 详情页的 `?namespace=` 由现有链接生成逻辑保留，自动选择不会覆盖已存在的 namespace。

替代方案是仅依赖接口返回的命名空间数量推断是否有集群级范围，但"只有一个命名空间"与"只有该命名空间的绑定"无法区分，且 `*` 绑定可能因 RBAC 返回空列表。使用 `me.bindings` 是平台层的准确信息。

### D5：集群级只读接口改用 `clusterVisible`

`apiResources`、`getCluster`、`getClusterHealth` 从 `*` 准入改为 `clusterVisible`；凭据字段仍只在 `PlatformAdmin` 时返回。`listEvents`、跨命名空间资源列表、WebSocket 订阅保持原语义（需要 `*` 或具体 namespace）。

## Risks / Trade-offs

- [`*` 用户的 K8s 身份没有 `list namespaces` 权限] → 接口返回 403 与上游原因；文档建议为其绑定包含 `namespaces` 只读权限的 ClusterRole，或在命名空间下拉出现空列表时提示联系管理员。
- [隐藏 Namespaces/Nodes 导航可能让有 K8s 权限但无平台 `*` 的用户困惑] → 与"平台绑定是可见范围门槛"的既有模型一致；直接访问 URL 仍得到明确 403。
- [自动选中会改写地址栏] → 使用 `replace`，不污染历史记录；只在首次进入且无 namespace 时触发。
- [绑定缓存导致短暂不一致] → `bindingsFor` 每请求读库，列表立即反映变更；`/auth/can` 的 30 秒缓存不影响列表。
- [开发模式无法直接验证非管理员行为] → 单元测试覆盖分支；UI 验证需要 OIDC 账号与对应绑定，文档记录该限制。

## Migration Plan

1. 后端先发布：新增判定为纯增量，命名空间级用户从 403 变为可用的过滤列表；管理员与 `*` 绑定用户行为不变。
2. 前端随后发布：依赖后端返回的过滤列表；旧后端上新前端仍可工作（只是选项较多）。
3. 回滚：回退前端即可恢复旧交互；后端回退不影响数据。
4. 无数据库变更，无配置项新增。

## Open Questions

- 是否需要在后续版本提供开发模式的非管理员身份开关，便于本地验证命名空间隔离？不影响本次规格与任务拆分。