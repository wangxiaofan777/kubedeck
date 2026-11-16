## ADDED Requirements

### Requirement: 日志跟随生命周期

日志跟随 SHALL 持续到容器结束、客户端断开或用户关闭跟随，MUST NOT 因请求级超时或服务端写超时被截断。服务端 SHALL 为跟随请求解除单次请求超时与写截止时间，并在界面明确区分「正常结束」与「异常断开」。

#### Scenario: 持续跟随超过一分钟

- **WHEN** 用户开启跟随后容器持续输出日志超过 60 秒
- **THEN** 服务端 SHALL 继续推送日志，客户端 SHALL 持续收到新行

#### Scenario: 容器退出

- **WHEN** 容器在跟随过程中退出
- **THEN** 流 SHALL 正常结束，已接收内容 SHALL 保留，界面 SHALL 标明流已结束

#### Scenario: 客户端断开

- **WHEN** 用户关闭页面或点击停止
- **THEN** 服务端 SHALL 在 5 秒内释放日志读取与连接资源