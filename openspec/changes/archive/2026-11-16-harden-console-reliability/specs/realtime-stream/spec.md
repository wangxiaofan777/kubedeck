## ADDED Requirements

### Requirement: 重新同步后的客户端恢复

客户端收到服务端重新同步指令后 MUST 重新拉取一次全量数据，并 MUST 使用空资源版本重新建立订阅。客户端 MUST NOT 在收到指令后继续保持一个已被服务端移除的订阅。重新订阅仅允许在当前连接仍打开时进行；连接已关闭时 SHALL 由既有重连与退避逻辑接管。

#### Scenario: 查询被过滤的列表

- **WHEN** 用户在带标签选择器的 Pod 列表页保持页面打开
- **THEN** 实时订阅 SHALL 携带同一标签选择器，标签不匹配的事件 MUST NOT 被合并进列表

#### Scenario: 服务端下发重新同步

- **WHEN** 服务端因资源版本过期或 watch 关闭下发 `resync`
- **THEN** 客户端 SHALL 重新拉取当前查询并重新发送订阅，后续事件 SHALL 继续到达

#### Scenario: 重新同步后无新事件

- **WHEN** 客户端完成重新订阅后 30 秒内没有资源变化
- **THEN** 订阅 SHALL 保持有效，MUST NOT 因一次重新同步永久停止推送