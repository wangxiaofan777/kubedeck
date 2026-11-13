## 1. 修复

- [x] 1.1 在 `internal/app/app.go` 仅在 `st != nil` 时创建审计 writer、启动其 worker，并把 writer 赋给 `Options.Audit`；验证：`go build ./...` 通过，且 `Options.Audit` 在无存储时保持 nil 接口
- [x] 1.2 无数据库启动 dev 模式并触发一次产生审计的请求（访问 `/api/v1/clusters` 自动创建 dev 会话），进程不 panic 且请求成功；验证：日志出现 `database not configured`，进程存活
- [x] 1.3 带数据库启动时审计仍正常写入：创建/登录后在 `/admin/audit-logs` 能看到记录；验证：审计条目数量增长
