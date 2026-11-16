## Context

当前 `RequireAuth` 在 `UNAUTHENTICATED` 时直接内联渲染 `LoginPage`；登录页只有 `href="/auth/login"`。开发模式下 `requireIdentity` 会在首个请求自动建会话，`/auth/login` 直接重定向 `/`，因此登录页没有真实入口。

资源详情由 `ResourceDetailPage` 一个组件通过 Ant Design Tabs 承载全部子视图；YAML 编辑组件内部再包含 Diff、dryRun 与提交。创建页把表单、YAML、预览左右并排放在同一屏。相关路由与数据获取集中在单个组件内，无法为子视图提供独立地址。

## Goals / Non-Goals

**Goals:**

- 登录页成为真实入口：能探测部署模式，能完成 OIDC 或开发登录，未配置时给出明确错误。
- 开发模式从“隐式自动登录”改为“显式登录”，减少误暴露风险。
- 资源详情的每个子视图有独立 URL、独立加载状态与浏览器历史。
- YAML 编辑与比较/提交分离；比较页成为提交前唯一入口。
- 创建流程按步骤拆分，刷新页面仍能从草稿恢复。

**Non-Goals:**

- 不改变 OIDC 授权码 + PKCE 流程、会话 Cookie 规则或服务端鉴权模型。
- 不引入前端路由框架替换 React Router，不引入全局状态库。
- 不改变后端资源写接口、Diff 接口或 dryRun 契约。
- 不把日志、事件、关联 Pod 拆成跨集群的独立一级页面；它们仍是资源详情的子视图。
- 不实现移动端适配。

## Decisions

### 1. 登录方式探测放在公开的 `GET /auth/mode`

响应只包含 `mode`（`oidc` / `dev` / `unconfigured`）、`issuer`（OIDC 时）、`devUser`（开发模式时）和固定的 `loginUrl`。不使用 `/api/v1/auth/me`，因为它在未登录时必然 401，登录页无法区分“未登录”和“未配置登录方式”。不返回 client secret、token 或任何凭据。

替代方案：让登录页盲目跳转 `/auth/login`，由后端失败后再跳回。无法在页面上提前说明原因，体验差且容易形成重定向循环。

### 2. 开发登录使用独立 `POST /auth/dev-login`

`requireIdentity` 不再在开发模式下自动建会话；所有受保护接口在无会话时统一返回 401。`POST /auth/dev-login` 仅在 `DevMode()` 为真时可用，建立与 OIDC 登录相同的 `MemorySessions` 会话并设置同样的 Cookie。非开发模式返回 404，避免暴露开发入口。

替代方案：保留自动登录，只在登录页加一个“开发模式”说明。这样登录页仍然不是真实入口，且任何能访问端口的客户端都会自动获得平台管理员身份，风险更高。

### 3. 登录页按模式渲染，不做本地密码表单

- `oidc`：展示 OIDC 按钮，`href` 指向 `/auth/login?returnTo=...`。
- `dev`：展示开发身份与“进入控制台”按钮，按钮调用 `POST /auth/dev-login`，成功后 `queryClient.clear()` 并跳转 `returnTo`。
- `unconfigured`：展示错误说明与管理员检查项，不渲染登录按钮。
- 探测请求失败：展示带重试的错误态，不静默降级为开发登录。

`RequireAuth` 改为 `<Navigate to={/login?returnTo=...} replace />`，登录页成为真实路由；`returnTo` 由服务端 `safeReturnTo` 再次校验，前端只做站内路径展示。

### 4. 资源详情采用“布局路由 + 子视图路由”

路由：

```
/clusters/:cluster/:typeKey/:namespace/:name                 → 概览（index）
/clusters/:cluster/:typeKey/:namespace/:name/yaml            → YAML 编辑
/clusters/:cluster/:typeKey/:namespace/:name/diff            → 变更比较
/clusters/:cluster/:typeKey/:namespace/:name/events          → 事件
/clusters/:cluster/:typeKey/:namespace/:name/logs            → 日志（仅 Pod）
/clusters/:cluster/:typeKey/:namespace/:name/pods            → 关联 Pod（有 selector 的非 Pod）
```

`ResourceDetailLayout` 负责：解析参数、拉取详情、403/404/加载/错误状态、页面头、子导航与 `<Outlet context>`。子视图只负责自己的数据（YAML、事件、日志、关联 Pod），避免概览页为未打开的 Tab 发起请求。子导航用 `NavLink` 渲染为轻量 tab 条，写入 `index.css`，不额外引入组件库。

替代方案：继续用 Tabs，但把每个 Tab 内容换成 `<Outlet />`。Ant Design Tabs 会为隐藏面板保留挂载与尺寸计算，Monaco 仍会遇到 0 宽布局问题，且 URL 与 Tab 状态容易失配，故不采用。

### 5. YAML 草稿用 `sessionStorage` 按对象键保存

键：`kubedeck:yaml-draft:<cluster>:<group>:<version>:<resource>:<namespace>:<name>`。值：`{ code, savedAt }`。编辑页点击“比较变更”时写入草稿并 `navigate('../diff')`；比较页读取草稿，没有草稿则展示空状态。提交成功后清除草稿并跳回概览；用户选择放弃时清除草稿并回到编辑页。

选择 `sessionStorage` 而非 Context：比较页与编辑页是兄弟路由，布局层不持有编辑状态；刷新后仍能恢复；关闭标签页自动清理，符合“一次性草稿”的语义。

### 6. 创建流程采用嵌套路由 + 布局持有草稿

```
/clusters/:cluster/new/:typeKey          → 配置步骤（index）
/clusters/:cluster/new/:typeKey/yaml     → YAML 步骤
/clusters/:cluster/new/:typeKey/review   → 确认步骤
```

`CreateResourceLayout` 持有 `values`、`code`、`copyFrom` 与 `discovery`，通过 `<Outlet context>` 向下传递；三个步骤页只渲染各自 UI。布局组件在挂载时从 `sessionStorage` 恢复草稿，在字段变化时写回；创建成功或取消时清除。无表单的类型（未知类型）在配置步骤直接跳转到 YAML 步骤。

替代方案：用 `?step=` 查询参数在单组件内切换。仍是一个组件承载全部逻辑，不符合“页面分开”的目标，且步骤间的前进/后退语义弱。

### 7. 比较页的提交前置条件

比较页必须满足：草稿存在、YAML 可解析、dryRun 已成功（或用户已确认）、`resourceVersion` 来自原对象。提交仍走现有 `PUT` 接口；冲突处理保持 409 + 刷新提示。比较页不提供“跳过比较直接提交”的入口。

## Risks / Trade-offs

- [开发模式自动登录被移除，现有脚本/验收命令会 401] → 更新 `docs/local-environment.md` 与验收步骤，明确先调用 `POST /auth/dev-login`；保留一次性迁移说明。
- [sessionStorage 草稿可能跨对象串用] → 草稿键包含集群、GVR、命名空间与名称；读取时校验 `savedAt` 与当前对象一致，超过 2 小时的草稿忽略。
- [路由拆分导致详情页重复拉取 detail] → 布局层只拉一次详情并通过 Outlet context 复用；子视图不再重复请求 detail。
- [创建流程状态分散在三个页面，容易出现前后不一致] → 布局层是唯一状态源；步骤页只调用 setter；确认页从同一 manifest 生成预览。
- [Monaco 在路由切换时重新挂载，增加首屏开销] → 继续懒加载；编辑页与比较页共用 `YamlCodeEditor`/`DiffEditor` 的懒加载边界。
- [登录页探测接口被滥用] → 响应仅含公开配置，无凭据；后续可在网关层限流，不阻塞本期。

## Migration Plan

1. 先发布后端：新增 `/auth/mode` 与 `/auth/dev-login`，移除开发模式自动建会话；旧前端在开发模式下会 401，属于预期行为。
2. 再发布前端：登录页与路由拆分。OIDC 部署不受影响；开发环境需在登录页点击一次开发登录。
3. 回滚：后端保留自动登录代码开关的成本较高，回滚时整体回退到上一版本；前端路由拆分为纯前端变更，可独立回滚。
4. 文档同步：更新本地环境、验收与运维文档中的登录步骤。

## Open Questions

- 是否需要在登录页展示 IdP 名称/图标？本期只展示 issuer 文本，图标留待有品牌资源时再加。
- 开发登录是否支持选择不同 `KUBEDECK_DEV_USER`？本期只使用配置中的单一开发身份，避免在页面上暴露任意身份切换。