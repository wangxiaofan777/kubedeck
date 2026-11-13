# workload-control Specification

## Purpose
把开发者日常最高频的工作负载操作（伸缩、重启、回滚、暂停、Pod 处理）封装为安全、可审计的单步动作，避免手工编辑 YAML 带来的格式错误与误操作。

## Requirements

### Requirement: 副本伸缩

系统 SHALL 支持调整 Deployment、StatefulSet、ReplicaSet 的副本数，且 MUST 通过 `scale` 子资源提交，以避免与主体更新冲突。

#### Scenario: 扩容

- **WHEN** 用户将 Deployment 副本数从 2 调整为 5
- **THEN** 系统 SHALL 提交新的副本数，并在界面上持续显示就绪副本的变化直至收敛或超时

#### Scenario: 缩容到零

- **WHEN** 用户将副本数设置为 0
- **THEN** 系统 SHALL 要求二次确认，并在确认后执行

### Requirement: 工作负载重启

系统 SHALL 支持重启 Deployment、StatefulSet、DaemonSet，重启 MUST 通过更新 Pod 模板触发新的滚动发布，MUST NOT 直接删除运行中的 Pod。

#### Scenario: 重启 Deployment

- **WHEN** 用户触发重启
- **THEN** 系统 SHALL 更新 Pod 模板的重启标记，并展示新 ReplicaSet 的滚动进度

#### Scenario: 重启被拒绝

- **WHEN** 用户对 Job 触发重启
- **THEN** 系统 SHALL 返回不支持的操作错误，并提示可执行的操作

### Requirement: 版本回滚

系统 SHALL 支持查看 Deployment 的历史版本并回滚到指定版本，回滚前 MUST 展示与当前版本的差异。

#### Scenario: 回滚到上一版本

- **WHEN** 用户选择历史版本并确认回滚
- **THEN** 系统 SHALL 将 Pod 模板恢复为该版本内容，并生成一个新的 revision

#### Scenario: 版本已不可用

- **WHEN** 用户选择的历史 revision 已被清理
- **THEN** 系统 SHALL 返回明确的不可用提示，且不修改当前工作负载

### Requirement: 暂停与恢复

系统 SHALL 支持暂停和恢复 Deployment 的滚动更新，并在暂停期间明确提示"变更不会自动推进"。

#### Scenario: 暂停滚动更新

- **WHEN** 用户暂停一个正在滚动的 Deployment
- **THEN** 系统 SHALL 停止推进新副本，并保持当前已就绪副本不变

### Requirement: Pod 删除与驱逐

系统 SHALL 支持删除单个或多个 Pod，并支持驱逐（Eviction）；驱逐遇到 PodDisruptionBudget 阻止时 MUST 返回阻止原因。

#### Scenario: 驱逐被 PDB 阻止

- **WHEN** 用户驱逐一个受 PDB 保护的 Pod
- **THEN** 系统 SHALL 返回 PDB 名称与当前允许中断数量，且不强制删除 Pod

#### Scenario: 删除由控制器管理的 Pod

- **WHEN** 用户删除属于 Deployment 的 Pod
- **THEN** 系统 SHALL 提示该 Pod 会被控制器重建，并在确认后执行

### Requirement: 操作结果反馈

所有工作负载操作 SHALL 返回即时受理结果，并提供后续状态查询，使用户能判断操作是否收敛。

#### Scenario: 操作收敛

- **WHEN** 伸缩或重启请求被接受
- **THEN** 系统 SHALL 在界面上展示目标状态、当前状态与耗时，并在收敛或失败时给出结论

### Requirement: 并发操作与幂等

系统 SHALL 对重复提交的同一操作做幂等处理，并在对象已被删除或状态不允许时返回明确错误。

#### Scenario: 重复点击重启

- **WHEN** 客户端因为超时重试同一次重启请求，携带相同幂等键
- **THEN** 系统 SHALL 只产生一次 Pod 模板变更，返回与首次一致的结果

#### Scenario: 并发伸缩

- **WHEN** 两个用户几乎同时提交不同的副本数
- **THEN** 系统 SHALL 以最后一次成功提交为准，并在响应中返回实际生效值

#### Scenario: 对象已删除

- **WHEN** 用户对已被删除的工作负载执行伸缩
- **THEN** 系统 SHALL 返回 404，不创建任何替代对象
