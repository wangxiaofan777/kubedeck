# realtime-stream Specification

## Purpose
通过单条多路复用长连接把集群资源变化增量推送到浏览器，使列表与详情页无需轮询即可保持一致，并在网络抖动或服务端压力下安全降级。

## Requirements

### Requirement: 多路复用订阅

系统 SHALL 允许客户端在单条 WebSocket 连接上订阅多个资源视图，每个订阅具备唯一标识并可按标识取消。

#### Scenario: 同时订阅多个视图

- **WHEN** 用户在 Pod 列表页打开一个 Deployment 详情
- **THEN** 系统 SHALL 在同一连接上新增一个订阅，且不影响已有订阅的事件流

#### Scenario: 取消订阅

- **WHEN** 用户关闭某资源视图
- **THEN** 系统 SHALL 停止推送该订阅的事件，并在 5 秒内释放对应资源

### Requirement: 增量事件

系统 SHALL 以新增、修改、删除三类增量事件推送资源变化，推送内容 MUST 仅包含列表与状态展示所需的摘要字段。

#### Scenario: 对象状态变化

- **WHEN** 某 Deployment 的就绪副本数发生变化
- **THEN** 客户端 SHALL 在 2 秒内收到该对象的修改事件，并更新对应行

#### Scenario: 对象被删除

- **WHEN** 用户订阅范围内有 Pod 被删除
- **THEN** 客户端 SHALL 收到删除事件，并从列表中移除该行，不需要重新拉取全量数据

### Requirement: 断线续传

连接中断后客户端 SHALL 能携带上次接收位置重新订阅；服务端无法从该位置续传时 MUST 明确要求客户端重新同步全量数据。

#### Scenario: 短暂断线

- **WHEN** 客户端在 30 秒内重连并提供上次的资源版本
- **THEN** 系统 SHALL 推送断线期间遗漏的变更，不要求全量刷新

#### Scenario: 无法续传

- **WHEN** 客户端提供的版本已超出服务端保留范围
- **THEN** 系统 SHALL 返回重新同步指令，客户端收到后重新拉取一次全量列表

### Requirement: 慢消费者保护

服务端 MUST 为每个连接设置有限队列；当客户端消费过慢导致队列溢出时，系统 SHALL 丢弃增量并下发重新同步指令，MUST NOT 无限缓冲。

#### Scenario: 队列溢出

- **WHEN** 某客户端长时间不消费事件且队列达到上限
- **THEN** 系统 SHALL 丢弃后续增量并通知该客户端重新同步

### Requirement: 连接配额

系统 SHALL 限制每个用户的并发实时连接数；超过配额时 MUST 拒绝新连接并给出明确原因。

#### Scenario: 超出连接上限

- **WHEN** 同一用户已建立达到上限的连接数并再次发起订阅
- **THEN** 系统 SHALL 拒绝新连接并提示关闭其他页面或稍后重试

### Requirement: 订阅权限

订阅 SHALL 复用调用者的身份与权限；当用户失去某资源的读取权限时，相关订阅 MUST 在 60 秒内被终止。

#### Scenario: 权限被回收

- **WHEN** 管理员回收了用户对某命名空间的读取权限
- **THEN** 系统 SHALL 终止该命名空间相关的订阅，且不再推送任何事件

### Requirement: 订阅生命周期边界

系统 SHALL 明确处理重复订阅、集群不可达与客户端主动关闭，保证订阅状态与连接状态一致。

#### Scenario: 重复订阅同一标识

- **WHEN** 客户端用已存在的订阅标识再次 `subscribe`
- **THEN** 系统 SHALL 以新参数替换旧订阅，不产生重复推送

#### Scenario: 集群不可达

- **WHEN** 订阅目标集群当前不可达
- **THEN** 系统 SHALL 返回 `CLUSTER_UNREACHABLE` 错误帧，不保持一个永无数据的订阅

#### Scenario: 客户端关闭页面

- **WHEN** 浏览器关闭导致连接断开
- **THEN** 服务端 SHALL 在 5 秒内释放该连接的全部订阅与队列
