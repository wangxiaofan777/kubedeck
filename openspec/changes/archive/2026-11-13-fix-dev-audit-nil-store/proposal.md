## Why

开发模式未配置 `KUBEDECK_DATABASE_URL` 时，服务仍会以 `nil` 的 `*store.Store` 构造审计 writer。Go 的接口持有 typed-nil 时 `w.store == nil` 为假，`Record` 把记录入队，`flushBatch` 调用 nil Store 的 `AppendAuditBatch` 直接 panic。开发模式下任何产生审计的请求（例如首次请求自动创建 dev 会话并记录 login）都会让进程退出，阻断「无数据库快速联调」。

## What Changes

- 仅在平台存储非空时创建并启动审计 writer；未配置数据库时 `Options.Audit` 保持真正的 nil。
- 不改变审计内容、队列语义或生产路径（生产必须配置数据库，行为不变）。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

（无，纯开发模式容错修复，`.openspec.yaml` 声明 `skip_specs: true`）

## Impact

- 仅 `internal/app/app.go` 的审计 writer 装配；无 API 契约、无数据库或配置变更。
