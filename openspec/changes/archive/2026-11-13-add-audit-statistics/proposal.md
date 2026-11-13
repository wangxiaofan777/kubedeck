## Why

审计日志目前只有分页列表与导出，平台管理员无法快速回答「最近有没有异常、失败集中在哪类操作、谁在频繁写」。安全复盘需要在控制台直接看到趋势与分布，而不是导出后人工统计。审计数据已经在 PostgreSQL 中按时间分区存储，补齐聚合读接口的成本低，也不需要新增存储或依赖。

## What Changes

- 新增 `GET /api/v1/admin/audit-logs/stats`：按时间范围（默认 7 天，最多 90 天）与可选集群聚合审计记录，返回总数、成功/失败数、按天趋势、高频动词、高频资源与高频操作者。
- 审计页增加概览卡片与分布表格，支持 7/30 天切换；不引入图表库。
- 统计仅返回聚合结果，不返回请求体、`detail` 或对象内容；仍要求 PlatformAdmin。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `audit-log`: 新增「审计统计」要求，定义时间范围、聚合维度、权限与不泄露内容的约束。

## Impact

- 后端：`internal/store/audit_stats.go`（聚合查询）、`internal/api/audit_stats.go`（处理器与参数校验）、`internal/api/router.go` 注册路由；复用现有 `AuditStore` 依赖。
- 接口契约：`docs/openapi.yaml` 增加统计操作。
- 前端：`web/src/lib/api.ts` 增加统计方法，`web/src/features/audit/AuditPage.tsx` 增加概览与分布展示。
- 数据库：无 schema 变更；依赖 `occurred_at` 的分区裁剪。
- 依赖：无新增。
