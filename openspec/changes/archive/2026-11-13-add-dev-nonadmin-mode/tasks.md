## 1. 配置与启动

- [x] 1.1 在 `internal/config/config.go` 增加 `DevPlatformAdmin bool`（yaml `dev_platform_admin`），`Default()` 设为 `true`，并解析 `KUBEDECK_DEV_PLATFORM_ADMIN`（只接受 true/false，非法值报错）；验证：`go test ./internal/config` 覆盖默认值与非法值
- [x] 1.2 在 `internal/app/app.go` 用配置值替代硬编码的 `PlatformAdmin: true`；验证：`go build ./...` 通过
- [x] 1.3 在 `docs/local-environment.md` 的变量表与示例中补充该开关及"仅开发模式"说明；验证：文档中变量名与代码一致

## 2. 验证

- [x] 2.1 默认启动（不设变量）后访问 `/api/v1/admin/users` 仍返回 200，确认默认行为未变
- [x] 2.2 以 `KUBEDECK_DEV_PLATFORM_ADMIN=false` 和 `KUBEDECK_DEV_USER=alice` 启动后访问 `/api/v1/admin/users` 返回 403，确认非管理员身份生效
- [x] 2.3 用该非管理员实例调用 `GET /clusters/local/overview`，确认统计只覆盖 alice 绑定命名空间；验证：对比管理员实例计数，并确认未绑定命名空间的 Pod 不计入