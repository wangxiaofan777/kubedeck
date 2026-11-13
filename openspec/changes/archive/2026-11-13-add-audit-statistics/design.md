## Context

`audit_logs` 是只追加的分区表，查询入口在 `internal/store/audit_query.go`，过滤条件已经覆盖时间、操作者、集群、资源与结果。统计只需要在这些条件上做 `GROUP BY`，不需要引入物化视图或缓存。

## Goals / Non-Goals

**Goals:** 用少量聚合查询给出管理员一眼可读的审计态势；范围可控、不泄露记录内容。

**Non-Goals:** 不做实时流式统计、不做跨租户对比、不引入图表库、不提供自定义 SQL/PromQL。

## Decisions

- **端点挂在 `/admin/audit-logs/stats`**：与现有审计查询、导出同属管理员能力，复用 `requireAdmin`。
- **范围参数**：`from`/`to` 接受 RFC3339；缺省为最近 7 天；跨度上限 90 天；`from >= to` 返回 400。`cluster` 可选，空值表示全部集群。
- **聚合维度**：总数与成功/失败、按天（`date_trunc('day')`）、动词 Top 20、资源 Top 10、操作者 Top 10。五个查询在同一事务外顺序执行，失败整体返回 500。
- **不返回明细**：响应只含计数与 key，不含 `detail`、`bodyHash` 或对象名。
- **前端不引入图表库**：用 antd `Statistic`、`Progress` 与 `Table` 展示；按天趋势用表格，避免为 P1 引入 ECharts。
- **口径**：`result = 'success'` 计为成功，其余计为失败；失败率 = 失败数 / 总数，总数为 0 时显示 0%。

## Risks / Trade-offs

- [大范围扫描慢] → 90 天上限 + `occurred_at` 过滤触发分区裁剪；必要时后续加索引或物化视图。
- [统计与列表口径不一致] → 共用 `AuditFilter` 的时间/集群条件，避免两套 SQL 漂移。
- [泄露敏感信息] → 只返回聚合计数与动词/资源/操作者标识，不返回请求体或对象内容。
