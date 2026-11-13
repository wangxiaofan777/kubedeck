# audit-log Specification

## Purpose
为所有改变集群状态或触及敏感数据的操作留下不可篡改、可查询的记录，使事故复盘与责任追溯有据可依，并为合规审计提供导出能力。

## Requirements

### Requirement: 记录范围

系统 MUST 记录所有写操作（创建、更新、补丁、删除、伸缩）、敏感读操作（查看 Secret 明文）、身份变更（登录、登出、角色绑定）以及凭据操作；只读列表浏览 MUST NOT 记录到操作审计中。

#### Scenario: 写操作被记录

- **WHEN** 用户删除一个 Deployment
- **THEN** 系统 SHALL 生成一条审计记录，包含操作者、集群、资源、命名空间、对象名、结果与耗时

#### Scenario: 只读浏览不入审计

- **WHEN** 用户浏览 Pod 列表
- **THEN** 系统 SHALL 不生成操作审计记录，仅在访问日志中保留请求元数据

### Requirement: 记录内容

每条审计记录 MUST 包含发生时间、操作者身份、来源 IP、集群、动作、资源类型、对象标识、结果状态码与请求追踪号；请求体 MUST 以摘要哈希形式保存，MUST NOT 保存明文内容。

#### Scenario: 敏感字段保护

- **WHEN** 用户创建一个包含 Secret 数据的对象
- **THEN** 审计记录 SHALL 仅保存请求体的摘要哈希，不保存字段值

#### Scenario: 失败操作同样记录

- **WHEN** 用户的删除请求被 apiserver 拒绝
- **THEN** 系统 SHALL 记录该次失败，包含拒绝原因与结果状态码

### Requirement: 不可变性

审计记录 MUST 只允许追加；系统 MUST NOT 提供修改或删除单条审计记录的接口，且相邻记录之间 SHALL 具备可校验的完整性关联。

#### Scenario: 拒绝篡改

- **WHEN** 任何客户端尝试修改或删除审计记录
- **THEN** 系统 SHALL 拒绝该操作，且不提供对应的 API 入口

#### Scenario: 完整性校验

- **WHEN** 管理员执行审计完整性校验
- **THEN** 系统 SHALL 报告记录链是否完整，并指出第一个校验失败的位置（如有）

### Requirement: 查询与保留

系统 SHALL 支持按时间范围、操作者、集群、资源类型与结果过滤审计记录，并支持分页；默认保留期 SHALL 不少于 180 天。

#### Scenario: 按对象追溯

- **WHEN** 管理员查询某 Deployment 的全部操作历史
- **THEN** 系统 SHALL 按时间倒序返回相关记录，并可继续翻页

#### Scenario: 超出保留期

- **WHEN** 查询时间范围早于保留期
- **THEN** 系统 SHALL 明确提示该范围数据已归档或不可用，而不是返回空结果

### Requirement: 导出

系统 SHALL 支持将审计查询结果导出为 CSV 或 JSON；导出行为本身 MUST 被记录。

#### Scenario: 导出审计

- **WHEN** 管理员导出某时间段的审计记录
- **THEN** 系统 SHALL 生成文件并新增一条"导出审计"的记录，包含导出条件与记录条数

### Requirement: 审计写入失败与时间一致性

审计写入失败 MUST NOT 导致业务操作失败；系统 SHALL 通过补偿队列最终写入，并统一以 UTC 存储、按用户时区展示。

#### Scenario: 数据库暂时不可用

- **WHEN** 审计表写入失败
- **THEN** 业务请求 SHALL 正常完成，审计事件进入本地补偿队列，恢复后补写并标记延迟

#### Scenario: 跨时区查询

- **WHEN** 用户以本地时区查询审计
- **THEN** 系统 SHALL 正确换算边界，结果与按 UTC 查询同一时间段一致

#### Scenario: 补偿队列溢出

- **WHEN** 补偿队列超过上限仍无法写入
- **THEN** 系统 SHALL 记录告警并拒绝新的高风险写操作（可配置为继续），不得静默丢弃审计

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
