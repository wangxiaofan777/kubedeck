## 1. 后端登录入口

- [x] 1.1 新增公开 `GET /auth/mode`，返回 `oidc` / `dev` / `unconfigured` 与可公开的提示信息，不包含任何凭据
- [x] 1.2 新增 `POST /auth/dev-login`，仅在开发模式可用，建立与其他登录方式一致的会话 Cookie
- [x] 1.3 移除 `requireIdentity` 在开发模式下的隐式自动建会话；无会话统一返回 `UNAUTHENTICATED`
- [x] 1.4 补充登录方式探测、开发登录、非开发模式拒绝开发登录的 handler 测试
- [x] 1.5 更新 `docs/local-environment.md`、`docs/acceptance.md` 中的开发登录步骤

## 2. 登录页

- [x] 2.1 `api.ts` 增加 `authMode()` 与 `devLogin()`，并补充类型
- [x] 2.2 重写 `LoginPage`：按模式渲染 OIDC 登录、开发登录或未配置错误；探测失败提供重试
- [x] 2.3 `RequireAuth` 未登录时重定向到 `/login?returnTo=...`，登录成功后返回原地址
- [x] 2.4 修正登录页中与实现不一致的授权描述
- [x] 2.5 补充 `safeReturnTo` 的站内路径、外链与协议相对地址测试

## 3. 资源详情子视图

- [x] 3.1 新增 `ResourceDetailLayout`：参数解析、详情查询、403/404/加载/错误状态、页面头、子导航与 Outlet context
- [x] 3.2 拆出 `ResourceOverviewPage`，保留元数据、容器与工作负载操作
- [x] 3.3 拆出 `ResourceYamlPage`：只负责编辑与保存草稿，提供“比较变更”入口
- [x] 3.4 拆出 `ResourceDiffPage`：读取草稿、展示 Diff/不可变字段、dryRun、冲突处理与提交
- [x] 3.5 拆出 `ResourceEventsPage` 与 `ResourceLogsPage`
- [x] 3.6 拆出 `ResourcePodsPage`，承载有 selector 的非 Pod 关联 Pod 列表
- [x] 3.7 更新 `App.tsx` 嵌套路由，保证旧详情地址仍落到概览页
- [x] 3.8 子导航样式写入 `index.css`，支持键盘焦点与当前页高亮

## 4. 创建流程

- [x] 4.1 新增 `CreateResourceLayout`，集中持有表单、YAML、发现结果与草稿恢复/清理
- [x] 4.2 拆出配置步骤页，保留模板、复制来源与表单校验
- [x] 4.3 拆出 YAML 步骤页，支持表单生成与手工编辑
- [x] 4.4 拆出确认步骤页，展示清单预览、执行 dryRun 并提交创建
- [x] 4.5 更新 `App.tsx` 创建流程嵌套路由；未知类型自动进入 YAML 步骤
- [x] 4.6 创建成功或取消时清除草稿，刷新页面可从草稿恢复

## 5. 验证

- [x] 5.1 后端：`go test ./cmd/... ./internal/...`；补充的登录测试通过
- [x] 5.2 前端：`npm run lint && npm run typecheck && npm run test && npm run build`
- [x] 5.3 手动走查：开发模式显式登录、未登录深链接返回、详情子视图前进/后退、YAML 草稿比较与提交、创建三步流程与创建落库
- [ ] 5.4 在配置了真实 OIDC 提供方的环境走查授权码登录与回跳（本机未配置 IdP）
- [x] 5.5 运行 `openspec validate refactor-console-pages-and-login --strict`