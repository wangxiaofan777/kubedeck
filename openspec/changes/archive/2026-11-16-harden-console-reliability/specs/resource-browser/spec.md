## ADDED Requirements

### Requirement: 驱逐执行时间

节点驱逐请求 SHALL 在操作自身的超时范围内执行，MUST NOT 被普通请求的 30 秒超时截断。默认超时 SHALL 为 120 秒，用户显式指定时 MUST 限制在 1–300 秒；超时后系统 SHALL 返回逐项结果并标明未处理的 Pod 为超时，而不是把整次操作伪装成成功。

#### Scenario: 长时间驱逐

- **WHEN** 节点上的 Pod 需要超过 30 秒才能完成驱逐
- **THEN** 请求 SHALL 继续执行到操作超时或全部处理完成，MUST NOT 在 30 秒时被全局请求超时中断

#### Scenario: 达到操作超时

- **WHEN** 驱逐达到用户指定的超时时间
- **THEN** 系统 SHALL 返回已完成、跳过、阻塞与超时的逐项统计，剩余 Pod SHALL 标记为超时

#### Scenario: 非法超时值

- **WHEN** 用户指定小于 1 秒或大于 300 秒的超时
- **THEN** 系统 SHALL 返回 400 `INVALID_ARGUMENT`，且不发起驱逐