## ADDED Requirements

### Requirement: 审计统计

系统 SHALL 为平台管理员提供审计聚合统计，按时间范围与可选集群返回总数、成功/失败数、按天趋势、高频动词、高频资源与高频操作者。统计 MUST 只返回聚合结果，MUST NOT 返回请求体、`detail` 或对象内容。

#### Scenario: 默认时间范围

- **WHEN** 管理员请求审计统计且未提供时间范围
- **THEN** 系统 SHALL 返回最近 7 天的聚合结果

#### Scenario: 统计失败率

- **WHEN** 时间范围内有成功与失败记录
- **THEN** 响应 SHALL 分别给出成功数、失败数与可按总数计算的失败率

#### Scenario: 按维度分组

- **WHEN** 管理员请求统计
- **THEN** 响应 SHALL 给出按天计数、动词 Top 20、资源 Top 10 与操作者 Top 10

#### Scenario: 非法时间范围

- **WHEN** `from` 晚于或等于 `to`，或跨度超过 90 天
- **THEN** 系统 SHALL 返回 400 `INVALID_ARGUMENT`，且不执行聚合查询

#### Scenario: 非管理员访问

- **WHEN** 非平台管理员请求审计统计
- **THEN** 系统 SHALL 返回 403 `FORBIDDEN`

#### Scenario: 空范围

- **WHEN** 时间范围内没有任何审计记录
- **THEN** 系统 SHALL 返回零计数与空分组，而不是错误