## 1. 传输层与错误映射

- [x] 1.1 在 `kube.Transport` 增加 `Patch(ctx, gvr, namespace, name, patchType types.PatchType, data []byte, opts PatchOptions)` 与 `PatchOptions{DryRun, FieldManager, Force}`，在 `DirectTransport` 中实现，并同步更新 `internal/api/handlers_test.go` 与 `internal/kube/registry_test.go` 的 fake transport；执行 `go build ./...`、`go test ./internal/kube/...` 与 `go test ./internal/api` 通过
- [x] 1.2 为 `FromUpstream` 增加 `apierrors.IsAlreadyExists → CodeConflict`、`apierrors.IsMethodNotSupported` 与 `apierrors.IsUnsupportedMediaType → CodeUnsupported` 映射，扩展 `internal/api/errors_test.go` 断言重名创建返回 409、上游 405/415 返回 400；执行 `go test ./internal/api -run TestFromUpstream` 通过
- [x] 1.3 在审计中间件的资源路径解析中增加 `http.MethodPatch → "patch"`，并补充测试断言 PATCH 请求记录为 `patch` 动词且 body 仅保存哈希；执行对应审计测试通过

## 2. 后端接口

- [x] 2.1 新增 `validAPIResource`（返回 `kube.APIResource`，含 `Namespaced`/`Kind`），让 `validGVR` 复用它；执行 `go test ./internal/api` 确认既有资源接口测试全部通过
- [x] 2.2 实现创建 handler：解析 YAML/JSON、校验 GVK/kind/name/命名空间规则与 discovery `create` 动词、拒绝 `resourceVersion`、透传 `dryRun`、返回 201（`format=yaml` 时返回 YAML），并注册 `POST /clusters/{cluster}/resources/{group}/{version}/{resource}`；新增测试覆盖成功、dryRun、名称冲突、命名空间约束、身份不一致、携带 resourceVersion、不支持创建的类型、413
- [x] 2.3 实现 PATCH handler：按 `Content-Type` 选择合并/策略合并/SSA、校验 `fieldManager` 与 `force`、校验请求体身份字段、透传 `dryRun`、返回 200；注册 `PATCH /clusters/{cluster}/resources/{group}/{version}/{resource}/{name}`；新增测试覆盖三种补丁、不支持的媒体类型、缺少 fieldManager、非 Apply 使用 force、SSA 冲突 409、身份不一致，以及上游 405/415 的错误映射
- [x] 2.4 更新 `docs/openapi.yaml`，补齐集合 `POST` 与单资源 `PATCH`（含查询参数、请求体媒体类型、响应码）；执行 `go test ./internal/api -run TestOpenAPIMatchesRoutes` 通过
- [x] 2.5 在 `handlers_test.go` 中断言创建/补丁请求经 impersonated transport 发出，并断言 `Patch` 收到正确的媒体类型、`fieldManager` 与 `force`；执行 `go test ./internal/api` 全部通过

## 3. 前端

- [x] 3.1 在 `web/src/lib/api.ts` 增加 `create`（POST，`application/yaml`，支持 `dryRun`）方法；执行 `npm --prefix web run typecheck` 通过
- [x] 3.2 从 `YamlEditor` 抽出可复用的编辑器展示层（Monaco 挂载、尺寸校正、只读状态），保持现有 YAML 编辑/预览/提交行为不变；手工验证详情页 YAML 编辑与 Diff 仍可用
- [x] 3.3 新增 `ResourceCreateModal`：按当前资源类型预填 `apiVersion`/`kind`/`metadata.name`/`metadata.namespace`，提供"服务端校验"（dryRun）与"创建"，成功后关闭弹窗、失效列表缓存并跳转详情；执行 `npm --prefix web run typecheck` 通过
- [x] 3.4 在 `ResourcesPage` 工具栏增加"新建"按钮：`useCan` 以 `create` 动词预检；已选命名空间按该命名空间预检；浏览全部命名空间且为命名空间级类型时隐藏按钮并显示"选择命名空间后可新建"提示；集群级类型按集群范围预检；无权限或类型不可创建时不展示；执行 `npm --prefix web run typecheck` 与 `npm --prefix web run lint` 通过
- [x] 3.5 在资源类型定义中增加 `creatable` 标记（Events、Nodes 为 false）与创建模板生成函数（仅身份字段），集群级资源不写入 `metadata.namespace`；执行前端单元测试或 typecheck 覆盖

## 4. 文档与验收

- [x] 4.1 更新 `docs/permission-matrix.md`，把 `create`/`patch` 从"仅 K8s RBAC 提及"对齐为平台实际暴露的接口；人工核对文档与路由一致
- [x] 4.2 执行 `make check`（后端 lint/测试 + 前端 lint/typecheck）全部通过
- [ ] 4.3 在 WSL k3s 环境做端到端验收：UI 创建 Deployment 成功并跳转详情；`curl` 以 `merge-patch+json` 修改 `spec.replicas` 成功；以 `apply-patch+yaml` 制造字段冲突返回 409，`force=true` 后成功；Viewer 角色看不到"新建"入口；YAML 编辑仍走 PUT 且过期提交返回 409；将结果记录到 `tasks.md` 备注

## 备注

- 4.2：Windows 环境没有 `make`，已执行等价命令：`gofmt -l cmd internal`、`go vet ./cmd/... ./internal/...`、`go test ./...`、`npm --prefix web run lint`、`npm --prefix web run typecheck`，全部通过。
- 4.3：后端与权限链路已实测（管理员实例 + alice 受限实例）：
  - `POST` 创建 Deployment 返回 201，对象可被 `kubectl` 读到；
  - `merge-patch+json` 修改 `spec.replicas` 返回 200 且副本数生效；
  - 对 kubectl 管理的 Deployment 执行 `apply-patch+yaml` 返回 409，`details.upstream` 含冲突字段 `.spec.replicas`，`force=true` 后返回 200；
  - alice（Viewer）调用创建返回 403；
  - `PUT` 携带当前 `resourceVersion` 返回 200，过期版本返回 409；
  - 审计记录出现 `create`/`patch`/`update` 动词，失败请求同样入库。
  - 浏览器侧已验证"新建"按钮可见、弹窗模板正确、提交请求到达服务端（空 `metadata.name` 返回 422）。**未完成部分**：IAB 自动化下 Monaco 的隐藏 textarea 与编辑模型不同步，无法通过自动化输入 YAML，因此"UI 创建成功后跳转详情"尚未在浏览器中跑通，需人工或真实浏览器补验后再勾选。
