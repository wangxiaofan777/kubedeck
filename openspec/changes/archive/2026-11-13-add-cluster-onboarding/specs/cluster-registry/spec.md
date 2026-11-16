## ADDED Requirements

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