## 1. 资源发现与解析

- [x] 1.1 新增 `web/src/features/resources/useApiResources.ts`：按集群查询 `GET /api-resources`，`staleTime` 5 分钟，暴露 `resources`、`loading`、`error` 与 `refetch`；验证：`npm run typecheck` 通过。
- [x] 1.2 在 `resourceTypes.ts` 增加纯函数：解析 `apiVersion` 为 group/version、按 `group+version+kind`（回退 `group+kind`）匹配发现结果、GVR 反查内置页面类型；验证：端到端覆盖 apps/v1、核心 v1（ConfigMap）与未知 `example.com/v1` 三类输入。
- [x] 1.3 解析实现需校验 `verbs` 是否包含 `create`，并返回可直接用于创建的 `{ gvr, namespaced, pageKey? }`；验证：`resolveCreateTarget` 在缺少 create 时返回「资源类型 X 不支持创建」。

## 2. 创建弹窗

- [x] 2.1 `ResourceCreateModal` 提交时解析 YAML 身份（`apiVersion`/`kind`/`metadata.name`/`metadata.namespace`），按解析结果选择目标 GVR 与命名空间；验证：Deployments 页创建 ConfigMap `ui-cm`，集群中 `kubectl get cm ui-cm` 成功。
- [x] 2.2 本地拦截：未发现目标、目标无 `create` 动词、集群级资源带 namespace、YAML 解析失败时展示明确错误且不发请求；验证：`kind: Widget` 与 `kind: Namespace` 带 namespace 两种场景均停在弹窗并显示原因，审计无新增记录。
- [x] 2.3 跨类型提示：YAML 目标与当前页类型不同时，弹窗内提示「将创建 X 并跳转到对应页面」；使用 300ms 防抖解析避免逐键解析；验证：注入 ConfigMap 后提示「YAML 目标是 ConfigMap（v1）…」，同类型时不出现。
- [x] 2.4 dryRun 与正式创建共用同一解析结果；验证：跨类型 ConfigMap 点「服务端校验」提示「通过（dryRun，ConfigMap）」，随后 `kubectl get cm ui-cm-dry` 为 NotFound，未持久化。
- [x] 2.5 目标命名空间规则：`metadata.namespace` 优先、缺省用当前命名空间，查询参数与请求体保持一致；验证：把 YAML namespace 改为 `kube-system` 后创建落到该命名空间并跳转 `/configmaps/kube-system/ui-ns-test`。

## 3. 创建成功后的导航

- [x] 3.1 `onCreated` 回调携带目标页面类型（能反查内置类型时）；`ResourcesPage` 按目标类型跳转详情页，并失效资源列表查询；验证：Deployments 页创建 ConfigMap 后 URL 为 `/clusters/local/configmaps/default/ui-cm`。
- [x] 3.2 目标类型未内置页面时不跳转，关闭弹窗并提示「已创建 {kind}/{name}」；验证：本地未构造 CRD，已检查 `pageKey` 为空时回调不导航的分支（代码审阅），无内置页面的真实创建留待后续 CRD 场景覆盖。
- [x] 3.3 同类型创建保持现有跳转与提示不变；验证：Deployments 页创建 `ui-created-2` 后进入其详情页，无跨类型提示。

## 4. 文档与验证

- [x] 4.1 本次变更不涉及接口契约与部署方式：`docs/openapi.yaml`、`docs/permission-matrix.md` 未改动，创建仍走既有集合接口；验证：`git diff` 确认文档无变化。
- [x] 4.2 `npm run lint`、`npm run typecheck` 通过；后端未改动，`go test ./cmd/... ./internal/...` 仍通过。
- [x] 4.3 浏览器端到端：跨类型创建 ConfigMap 成功并跳转详情；未知 kind 本地拦截；集群级带 namespace 本地拦截；同类型创建回归；跨类型无权限沿用服务端 403 的既有错误展示（此前 viewer 403 已由创建变更的端到端覆盖）。
- [x] 4.4 清理测试对象（`ui-cm`、`ui-created-2`、`ui-ns-test`、dryRun 无残留），确认审计只记录实际发出的 create（configmaps、deployments 各一次），被本地拦截的请求未产生审计。
- [x] 4.5 `openspec validate fix-create-resource-kind --strict` 通过后归档、提交并推送。