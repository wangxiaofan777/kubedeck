# cluster-registry Specification

## Purpose
统一登记 KubeDeck 可管理的 Kubernetes 集群，安全保存接入凭据，并持续评估集群可用性，为其余能力提供集群级的访问边界与降级依据。

## Requirements

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

### Requirement: kubeconfig 文件引用拒绝

系统 MUST 拒绝 kubeconfig 中通过文件路径引用凭据或证书的字段（`tokenFile`、`certificate-authority`、`client-certificate`、`client-key`），只接受内联数据，避免服务端读取任意本地文件。

#### Scenario: 提交 tokenFile

- **WHEN** 管理员提交包含 `tokenFile: /var/run/secrets/...` 的 kubeconfig
- **THEN** 系统 SHALL 返回 400，说明不支持文件路径凭据，且不发起连接

#### Scenario: 提交内联 token

- **WHEN** 管理员提交包含内联 `token` 的 kubeconfig
- **THEN** 系统 SHALL 正常解析并执行接入探测

### Requirement: 集群状态消息脱敏

对外暴露的集群状态消息 SHALL 只描述错误分类（不可达、凭据无效、证书校验失败），MUST NOT 包含内部地址、DNS 名或原始网络错误。原始错误 SHALL 写入服务端日志。

#### Scenario: 探测失败

- **WHEN** 健康探测因 `dial tcp 10.0.0.5:6443` 失败
- **THEN** 集群详情 SHALL 显示「集群不可达」，不得显示目标 IP 与端口

### Requirement: 集群接入界面

系统 SHALL 为平台管理员提供图形化集群接入流程，支持 kubeconfig 与 server + token 两种方式，并在创建前完成连通性、版本与权限预检。预检 MUST NOT 写入数据库或返回凭据内容。非平台管理员 MUST NOT 看到接入入口。

#### Scenario: 管理员接入集群

- **WHEN** 平台管理员提交有效 kubeconfig
- **THEN** 界面 SHALL 展示探测到的版本与耗时，确认后创建集群并跳转到集群设置页

#### Scenario: 预检失败

- **WHEN** 目标集群不可达或凭据无效
- **THEN** 界面 SHALL 停留在当前步骤，展示失败分类与修复建议，且不创建集群记录

#### Scenario: 非管理员访问

- **WHEN** 非平台管理员直接访问接入页面
- **THEN** 系统 SHALL 展示 403 状态页，且不渲染凭据表单

### Requirement: 集群管理界面

系统 SHALL 提供集群列表与设置页，展示健康状态、版本、凭据元数据，并支持轮换凭据、修改展示信息、删除集群与管理访问绑定。任何界面 MUST NOT 回显凭据明文。

#### Scenario: 轮换凭据

- **WHEN** 管理员提交新的有效凭据
- **THEN** 界面 SHALL 更新凭据元数据与健康状态，旧凭据立即失效

#### Scenario: 轮换失败

- **WHEN** 新凭据无法连通目标集群
- **THEN** 系统 SHALL 保留旧凭据并展示失败原因

#### Scenario: 删除集群

- **WHEN** 管理员删除集群
- **THEN** 界面 SHALL 要求输入集群名称确认，并明确说明不会删除目标集群资源

#### Scenario: 管理访问绑定

- **WHEN** 管理员为某用户授予集群或命名空间角色
- **THEN** 绑定 SHALL 在 60 秒内对该用户生效，列表 SHALL 立即刷新

#### Scenario: 无集群时的首页

- - **WHEN** 平台没有接入任何集群
- **THEN** 管理员首页 SHALL 展示接入引导，非管理员 SHALL 展示等待管理员接入的说明
