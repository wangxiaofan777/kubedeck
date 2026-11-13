## Purpose

统一登记 KubeDeck 可管理的 Kubernetes 集群，安全保存接入凭据，并持续评估集群可用性，为其余能力提供集群级的访问边界与降级依据。

## ADDED Requirements

### Requirement: 集群接入

系统 SHALL 允许平台管理员通过 kubeconfig 或 ServiceAccount Token 接入集群，且仅接受 HTTPS 的 apiserver 地址。

#### Scenario: 使用 kubeconfig 接入

- **WHEN** 管理员提交包含有效凭据与 `https://` 地址的 kubeconfig
- **THEN** 系统 SHALL 在 5 秒内完成连通性探测，并在集群列表返回 `status=healthy`

#### Scenario: 拒绝不安全的地址

- **WHEN** 提交的 server 地址为 `http://`，或指向链路本地地址段 `169.254.0.0/16`
- **THEN** 系统 SHALL 拒绝创建并返回 400，且错误信息说明被拒绝的原因

### Requirement: 集群版本记录与支持边界

系统 SHALL 在接入探测时读取目标集群的版本并随集群信息暴露；对版本低于平台声明的最低支持版本的集群，系统 SHALL 拒绝接入。

#### Scenario: 接入时记录版本

- **WHEN** 管理员接入一个可连通的集群
- **THEN** 探测 SHALL 读取该集群的版本信息并保存，集群列表与详情 SHALL 展示该版本

#### Scenario: 低于最低支持版本

- **WHEN** 接入的集群版本低于平台声明的最低支持版本
- **THEN** 系统 SHALL 返回 400，错误信息 SHALL 说明平台声明的最低支持版本，且不创建集群记录

#### Scenario: 集群升级后的版本刷新

- **WHEN** 已接入集群升级到新版本后，健康检查读取到新的版本信息
- **THEN** 集群列表与详情展示的版本 SHALL 更新为新版本

### Requirement: 凭据保护

系统 MUST 使用 AES-256-GCM 加密存储集群凭据；任何 API 响应、日志与审计记录中 MUST NOT 出现凭据明文或密文。

#### Scenario: 凭据脱敏

- **WHEN** 客户端读取集群详情
- **THEN** 响应 SHALL 仅包含 `server`、`auth_mode`、凭据指纹与轮换时间，不包含 token、client-key 或证书私钥

#### Scenario: 凭据轮换

- **WHEN** 管理员提交新的集群凭据
- **THEN** 系统 SHALL 以新的密钥版本覆盖旧凭据并记录轮换时间，旧凭据立即失效

### Requirement: 健康检查

系统 SHALL 至少每 60 秒探测一次已接入集群的 `/readyz` 并读取集群版本，对外暴露 `healthy`、`degraded`、`unreachable` 三种状态。

#### Scenario: 连续失败降级

- **WHEN** 同一集群连续 3 次探测失败
- **THEN** 状态 SHALL 变为 `unreachable`，且该集群的读取请求返回明确的不可用错误，而不是无限等待

#### Scenario: 恢复

- **WHEN** 不可达集群重新探测成功
- **THEN** 状态 SHALL 在 2 个探测周期内恢复为 `healthy`

### Requirement: 集群可见范围

系统 SHALL 仅向用户展示其角色绑定覆盖的集群；未授权集群 MUST NOT 出现在列表、搜索或错误信息中。

#### Scenario: 无绑定用户

- **WHEN** 用户没有任何集群绑定
- **THEN** 集群列表 SHALL 返回空数组，而不是返回全部集群

#### Scenario: 未授权访问

- **WHEN** 用户请求其无绑定的集群
- **THEN** 系统 SHALL 返回 403，且响应中不包含该集群的名称与地址

### Requirement: 集群移除

移除集群 SHALL 要求二次确认，且 MUST NOT 删除或修改目标集群中的任何资源。

#### Scenario: 移除集群

- **WHEN** 管理员确认移除某集群
- **THEN** 系统 SHALL 停止该集群的健康探测与缓存，并将其从列表移除，相关审计记录 SHALL 保留
### Requirement: 接入边界与冲突处理

系统 SHALL 拒绝重复接入同名集群，并在已接入集群的凭据失效时明确提示轮换，而非静默失败。

#### Scenario: 重复接入同名集群

- **WHEN** 管理员提交的集群名称已存在
- **THEN** 系统 SHALL 返回 409，并提示使用「轮换凭据」或先移除旧集群

#### Scenario: 凭据失效

- **WHEN** 目标集群返回 401/403 或证书校验失败
- **THEN** 集群状态 SHALL 变为 `degraded`，错误信息指向凭据问题，且其他集群不受影响

#### Scenario: 并发接入

- **WHEN** 两个管理员同时提交同名集群
- **THEN** 系统 SHALL 只创建一个集群记录，另一个请求返回 409
