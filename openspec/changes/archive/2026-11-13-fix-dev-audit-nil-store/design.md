## Context

`audit.NewWriter` 的参数是接口类型；把 `(*store.Store)(nil)` 传进去后接口值非 nil，`Record` 的空存储判断失效。生产路径必须配置数据库，因此问题只出现在无数据库的 dev 模式。

## Goals / Non-Goals

**Goals:** 无数据库的 dev 模式稳定运行，审计静默禁用而不是 panic。

**Non-Goals:** 不改变审计写入、重试、降级语义；不引入内存审计存储。

## Decisions

- **在 app 装配层条件化**：只有 `st != nil` 时才创建 writer、启动 `Run` 并把 writer 放入 `Options.Audit`。相比在 `audit.Writer` 内用反射识别 typed-nil，装配层判断更直白，也不给审计包引入反射。
- **不把 nil 具体类型赋给接口字段**：`Options.Audit` 只在有存储的代码块内赋值，确保中间件看到的是真正的 nil 接口。

## Risks / Trade-offs

- [生产漏配数据库] → 生产启动校验仍要求数据库，且日志会提示 `database not configured`；本次修复不放松该校验。
- [未来新增审计入口忘记判断] → 装配层是唯一构造点；保持单点判断即可。
