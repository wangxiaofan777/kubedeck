## ADDED Requirements

### Requirement: 会话与部署副本一致性

平台 SHALL 保证登录会话在承担同一入口流量的全部服务副本间有效。若当前实现只支持进程内会话，部署清单 MUST 拒绝大于 1 的副本数，并在渲染阶段给出明确原因，MUST NOT 静默生成会导致随机认证失败的多副本部署。

#### Scenario: 默认单副本

- **WHEN** 使用默认部署配置渲染清单
- **THEN** 渲染 SHALL 成功，副本数 SHALL 为 1

#### Scenario: 显式配置多副本

- **WHEN** 部署配置把副本数设置为大于 1，且没有共享会话存储
- **THEN** 清单渲染 SHALL 失败并说明会话存储限制，MUST NOT 生成多副本 Deployment