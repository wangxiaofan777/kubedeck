## Context

当前资源写路径只有 `PUT`（全量替换，要求 `resourceVersion`）与 `DELETE`，`kube.Transport` 已有 `Create` 实现但未接线，也没有 `Patch`。路由、OpenAPI、前端 API 客户端与页面入口需要同步扩展；apiserver 通过 impersonation 完成最终授权，平台不维护第二套权限表。

现有约定需要延续：资源地址为 `{group}/{version}/{resource}`（core 组用 `_`），命名空间走 `namespace` 查询参数，单个请求体上限 1 MB，错误响应统一为 `{code, message, details, requestId}`，写操作由审计中间件记录动词与请求体哈希。

## Goals / Non-Goals

**Goals:**

- 用最小新增面接通创建与部分更新：`POST` 集合路径、`PATCH` 单对象路径，复用现有 GVR 校验、YAML 解析、错误映射与审计链路。
- 创建与补丁的错误语义可预测：冲突、身份不一致、类型不支持、超限分别映射到稳定错误码。
- 前端能够在当前资源类型下完成"新建 → 服务端校验 → 提交 → 进入详情"的闭环。

**Non-Goals:**

- 不做 `metadata.generateName` 的自动命名创建；创建必须显式提供 `metadata.name`。
- 不实现 JSON Patch（`application/json-patch+json`）；只支持设计文档列出的合并、策略合并与 SSA。
- 不做表单化创建器（按 CRD schema 生成表单），新建入口统一走 YAML。
- 不扩展前端资源目录到 discovery 驱动的任意 GVR 浏览；新建入口仍限定在现有 13 种资源类型。
- 不把 `Idempotency-Key` 语义扩展到创建/补丁；重复创建由 `409 AlreadyExists` 表达。
- 不改变现有 `PUT` 更新语义，也不在控制台提供 SSA 入口；YAML 编辑继续使用 `PUT` + `resourceVersion` 乐观锁。

## Decisions

### D1：路由形态对齐设计文档附录 D

`POST` 挂在资源集合路径，`PATCH` 挂在单对象路径，与 `k8s-console-design.md` D.3 一致：

```text
POST   /api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}
PATCH  /api/v1/clusters/{cluster}/resources/{group}/{version}/{resource}/{name}
```

替代方案是把创建做成 `/resources/{...}/create` 子路径，但会偏离已公布的接口清单，且无法表达 REST 集合语义。选择集合 `POST` 的代价是必须依赖 discovery 判断资源是否命名空间级。

### D2：创建 handler 复用现有校验，新增资源元数据查询

- 复用 `decodeYAMLBody`（1 MB 上限、YAML/JSON 解析、数字规范化）与 `validObjectName`。
- `validGVR` 只返回 `schema.GroupVersionResource`，创建需要 `Namespaced` 与 `Verbs`。新增 `validAPIResource` 返回 `kube.APIResource`，`validGVR` 改为其薄包装，避免影响现有调用点。
- 命名空间规则：命名空间级资源必须提供非空 `namespace` 查询参数；集群级资源必须不提供。请求体 `metadata.namespace` 若存在必须与查询参数一致。
- 身份规则：`apiVersion` 的 group/version、`kind` 必须与 discovery 中该 GVR 的 Kind 一致；`metadata.name` 必填且必须合法（集合路径没有 name）。
- 若 discovery 的 `Verbs` 非空且不含 `create`，返回 400 `UNSUPPORTED`，避免上游 405 被归为 500。
- 拒绝带 `metadata.resourceVersion` 的创建请求（422），避免把"更新意图"误当创建。
- `dryRun=All` 透传给 apiserver；成功返回 201，`?format=yaml` 时返回 YAML 文本，与现有更新接口保持对称。
- `kind` 校验需要 discovery 的 `Kind` 字段；若发现结果未提供 Kind（老集群/聚合 API 异常），退化为只校验 group/version，避免误拒。

替代方案是只校验 `apiVersion` 不校验 kind，但会把错误 Kind 透传给 apiserver 并得到难懂的错误；显式校验更符合"提交前给出明确错误"的既有风格。

### D3：PATCH 由媒体类型驱动，不猜默认类型

- 支持的媒体类型严格限定三种：
  - `application/merge-patch+json`
  - `application/strategic-merge-patch+json`
  - `application/apply-patch+yaml`
- 用 `mime.ParseMediaType` 解析（忽略 charset 参数）；缺失或不支持时返回 400 `UNSUPPORTED`，不默认成合并补丁。
- `fieldManager` 查询参数：Apply 必须非空；其他类型缺省为 `kubedeck`。
- `force=true` 只允许用于 Apply；用于其他类型返回 400，避免调用方误解语义。
- `dryRun=All` 透传。
- 请求体同样限制 1 MB；Apply 必须是完整对象（含 `apiVersion`、`kind`、`metadata.name`），合并类补丁允许只含片段。
- 身份校验：请求体若携带 `metadata.name`、`metadata.namespace`、`apiVersion`、`kind`，必须与路径/查询参数一致；未携带则跳过。
- 返回 200 与更新后的对象；`?format=yaml` 返回 YAML。
- SSA 冲突由 apiserver 返回 409，`FromUpstream` 已有 `IsConflict` 映射，保留 `details.upstream` 供前端展示冲突字段。

替代方案是让 YAML 编辑器改用 SSA，但编辑器提交的是从 GET 得到的完整 live 对象（含 apiserver 默认值、`status`、`managedFields`）。apply 会让平台成为所有这些字段的 field manager：对 Helm / kubectl / operator 管理的对象会频繁产生字段冲突，`force=true` 又会静默接管本不属于平台的字段；同时 SSA 不使用 `resourceVersion` 做乐观锁，现有"过期提交 → 409"的保护会消失。因此控制台保留 `PUT`，SSA 只对明确知道字段所有权的 API 调用方开放。

### D4：传输层新增 `Patch`，创建直接复用 `Create`

`kube.Transport` 增加：

```go
Patch(ctx, gvr, namespace, name string, patchType types.PatchType, data []byte, opts PatchOptions) (*unstructured.Unstructured, error)
```

`DirectTransport.Patch` 调用 `dynamic.Resource(gvr).Namespace(ns).Patch(...)`，`PatchOptions` 承载 `DryRun`、`FieldManager`、`Force`。`types.PatchType` 直接用 `k8s.io/apimachinery/pkg/types` 的常量，避免自定义一套字符串枚举。

创建路径不需要新传输方法，直接调用已存在的 `DirectTransport.Create`。

### D5：补齐两个错误映射缺口

- `FromUpstream` 当前没有 `apierrors.IsAlreadyExists` 分支，创建重名对象会被归为 `INTERNAL/500`。增加映射到 `CONFLICT/409`。
- 同样缺少 `apierrors.IsMethodNotSupported` 与 `apierrors.IsUnsupportedMediaType`：CRD 不支持策略合并、资源不支持创建/补丁时会落到 `INTERNAL/500`。两者分别映射到 `UNSUPPORTED/400`。
- 审计中间件的资源路径解析只处理 `GET/PUT/POST/DELETE`，`PATCH` 会得到空动词。增加 `http.MethodPatch → "patch"`，确保规格中的审计要求可验证。

### D6：前端新建入口放在资源列表，YAML 编辑器抽成可复用表面

- `ResourcesPage` 工具栏增加"新建"按钮，权限预检使用 `useCan({verb: 'create'})`：
  - 已选命名空间时，按该命名空间预检；
  - 浏览全部命名空间且当前类型为命名空间级时隐藏按钮，并在工具栏显示"选择命名空间后可新建"的被动提示（不是可点击入口）；
  - 集群级类型按集群范围预检，只有集群级绑定或平台管理员可见。
- 资源类型增加显式 `creatable` 标记，Events / Nodes 不提供新建入口（它们通常由 kubelet 或控制器创建）；后端仍以 discovery 的 `Verbs` 兜底拒绝。
- 新建弹窗复用 Monaco 编辑器。为避免复制 `YamlEditor` 中大量布局修正逻辑，抽出展示层组件（编辑器挂载、尺寸校正、只读/可用状态），`YamlEditor` 与新建弹窗共用；新建模式不请求既有 YAML、不展示 Diff。
- 模板只预填 `apiVersion`、`kind`、`metadata.name`（空）、命名空间级资源的 `metadata.namespace`；不预填 `spec`，避免生成看似可提交但实际无效的对象。
- 弹窗提供"服务端校验"（`dryRun=All`）与"创建"两个动作；创建成功后关闭弹窗、失效列表缓存并跳转详情页。
- `api.ts` 增加 `create` 方法；不增加没有 UI 消费者的 `patch` 客户端方法，PATCH 由 API 调用方直接使用。

替代方案是直接复制 `YamlEditor` 布局代码到新弹窗，改动更小但会把 Monaco 的尺寸 workaround 变成两份，后续容易只修一边。

### D7：权限仍由 apiserver 判定

平台只做"是否可访问该集群/命名空间"的绑定检查与前端入口可见性预检；`create`/`patch` 的最终授权由 impersonated 身份在 apiserver 完成。写请求失败时按 `403 FORBIDDEN` 返回，不在平台侧模拟 RBAC。

## Risks / Trade-offs

- [CRD 不支持策略合并补丁] → 由 apiserver 返回 415/405，`FromUpstream` 映射为 400 `UNSUPPORTED`；OpenAPI 与文档注明仅内置资源保证策略合并语义。
- [SSA 对 Helm/kubectl 管理的对象易产生字段冲突] → 控制台不提供 SSA 入口；API 文档注明字段所有权与 `force` 语义，调用方自行决定是否接管。
- [SSA 冲突信息依赖上游 message] → 保留 `details.upstream`，前端不解析具体字段，只提示冲突并建议查看 managedFields；后续可结构化。
- [创建入口在"全部命名空间"下对按命名空间授权的用户隐藏] → 属预期：先选择命名空间即可创建；在空状态提示中说明。
- [`kind` 校验依赖 discovery] → discovery 未提供 Kind 时退化为 group/version 校验，避免误拒。
- [新增写入口扩大攻击面] → 全部走 impersonation；请求体 1 MB 上限；审计记录哈希；现有 `requireIdentity` 与绑定检查不变。
- [前端新建模板过于空泛] → 只预填身份字段，服务端校验负责给出具体错误；后续可基于 OpenAPI schema 生成模板。

## Migration Plan

1. 先发布后端（新增路由为纯增量，旧客户端不受影响）。
2. 再发布前端（依赖新路由；旧前端在新后端上行为不变）。
3. 回滚：移除前端入口即可停用；后端路由可随版本回退，不涉及数据迁移。
4. 无数据库 schema 变更，无配置项新增。

## Open Questions

- 是否需要在后续版本支持 `generateName` 与 JSON Patch？不影响本次规格与任务拆分，可单独提变更。