## Why

当前控制台把对象概览、YAML 编辑、Diff 比较、事件、日志、关联 Pod 全部堆在资源详情页的 Tabs 里；新建资源也把表单、YAML 编辑和清单预览挤在同一屏。任务之间没有独立地址，无法深链接、无法分别返回，页面显得拥挤且难以审计。

登录页目前只是一个静态跳转页：开发模式下 `/auth/login` 直接重定向首页，任何 API 请求都会自动建立会话，登录页实际不可达；OIDC 模式下按钮跳转是真实的，但页面无法反映后端是否配置了登录方式，未配置时点击后只会回到登录页。用户因此认为“登录页是假的”。

## What Changes

- **BREAKING（仅开发模式）**：开发模式不再在首个 API 请求时自动建立会话；新增显式开发登录入口，浏览器必须先完成一次开发登录，CLI/脚本需先调用开发登录接口。
- 新增公开的登录方式探测接口，登录页据此展示 OIDC 登录、开发登录或“未配置登录方式”的明确状态。
- 登录页改为真实入口：OIDC 模式跳转 IdP；开发模式提交本地开发身份；未配置时展示可操作的错误说明，不再渲染无法工作的按钮。
- 未登录访问受保护路由时重定向到 `/login?returnTo=...`，登录成功后回到原地址。
- 资源详情拆成独立路由：概览、YAML 编辑、变更比较、事件、日志、关联 Pod 各自一页，顶部子导航切换。
- YAML 编辑与 Diff 比较分离：编辑页保存草稿并进入比较页；比较页负责 dryRun、冲突提示与提交。
- 新建资源拆成独立步骤页：配置表单 → YAML 高级编辑 → 清单确认与提交；步骤之间可前进/后退，草稿在本次创建流程内保留。
- 修正登录页中与实现不一致的授权描述，改为准确表达“平台准入范围 + 目标集群 RBAC 双重校验”。

## Capabilities

### New Capabilities

- 无。

### Modified Capabilities

- `identity-access`: 登录入口的行为变化——新增登录方式探测、显式开发登录、未登录重定向与 OIDC 不可用时的明确反馈。
- `web-console`: 资源详情与创建任务的页面层级变化——从单页 Tabs 拆为独立路由；YAML 编辑与 Diff 比较分离；创建流程拆为步骤页。

## Impact

- 后端：`internal/api/auth.go`、`internal/api/router.go`、`internal/auth`（登录方式与开发会话）、`internal/api` 登录相关测试。
- 前端：`web/src/features/auth/LoginPage.tsx`、`web/src/app/App.tsx`、`web/src/app/AuthProvider.tsx`、`web/src/lib/api.ts`、`web/src/features/resources/*`、`web/src/app/ConsoleShell.tsx`、`web/src/index.css`。
- 路由：资源详情新增子路由；新建资源改为嵌套步骤路由。
- 文档：`docs/local-environment.md`（开发登录方式）、`docs/acceptance.md`（登录验收步骤）。
- 不涉及数据库迁移，不改变 OIDC 授权码 + PKCE 流程本身。