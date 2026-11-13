## 1. 数据层

- [x] 1.1 在 `internal/store/audit_stats.go` 实现 `AuditStats` 聚合：总数/成功/失败、按天、动词 Top 20、资源 Top 10、操作者 Top 10，复用时间与集群过滤；验证：`go build ./...` 通过，并在 `make test-db` 中新增用例覆盖空范围与有数据范围
- [x] 1.2 确认 `occurred_at` 过滤会触发分区裁剪（EXPLAIN 显示只扫描目标分区）；验证：在本地库对 90 天查询执行 EXPLAIN，记录结果到备注

## 2. 接口

- [x] 2.1 新增 `internal/api/audit_stats.go`：解析 `from`/`to`/`cluster`，默认 7 天、上限 90 天，调用聚合并序列化；验证：处理器单测覆盖默认范围、非法范围与空结果
- [x] 2.2 在 `internal/api/router.go` 的 admin 路由下注册 `GET /admin/audit-logs/stats`，并扩展 `AuditStore` 接口；验证：`go test ./internal/api` 通过
- [x] 2.3 在 `docs/openapi.yaml` 增加该操作与响应 schema；验证：`go test ./internal/api -run TestOpenAPIMatchesRoutes` 通过
- [x] 2.4 非管理员访问返回 403；验证：处理器测试或路由测试断言

## 3. 前端

- [x] 3.1 在 `web/src/lib/api.ts` 增加统计类型与 `auditStats` 方法；验证：`npm run typecheck` 通过
- [x] 3.2 在 `AuditPage` 顶部增加 7/30 天切换、总数/失败率/成功失败卡片，以及按天、动词、资源、操作者四个分布表格；验证：`npm run lint && npm run typecheck` 通过
- [x] 3.3 空数据与加载失败分别展示空状态与可重试错误，不影响下方审计列表；验证：本地页面手工切换范围并观察

## 4. 验收

- [x] 4.1 后端全量测试通过；`gofmt -l cmd internal`、`go vet` 无输出
- [x] 4.2 本地 k3s + PostgreSQL 环境请求统计接口，计数与 `audit_logs` 直接查询一致；验证：对比 `psql` 聚合结果
- [x] 4.3 前端构建通过，审计页在 7/30 天切换下展示正确且不阻塞列表；验证：`npm run build` 与浏览器检查

## 备注

- 1.2：本地库 `EXPLAIN` 显示 `Append ... Subplans Removed: 3`，仅扫描 `audit_logs_202610`、`audit_logs_202611`、`audit_logs_default`，90 天范围不会扫描全部历史分区。
- 4.2：`GET /admin/audit-logs/stats`（默认 7 天）返回 `total=247, success=243, failed=4`，与 `psql` 同一时间范围聚合 `247|243|4` 完全一致；带 `cluster=local` 时为 14（登录类记录无集群名，符合预期）。
- 4.3：浏览器实测审计页显示总数/成功/失败/失败率与按天、动词、资源、操作者四个分布表格；切换 7/30 天后自动重新请求并展示"最近 30 天"；下方审计列表正常分页展示，未被统计请求阻塞。
