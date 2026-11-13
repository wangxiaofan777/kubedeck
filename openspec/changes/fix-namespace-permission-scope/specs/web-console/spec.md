## MODIFIED Requirements

### Requirement: 上下文导航

控制台 SHALL 以「集群 → 命名空间 → 资源」为主导航层级，用户 SHALL 能随时看到并切换当前上下文，切换后所有视图 MUST 跟随新上下文刷新。命名空间选择器 SHALL 仅展示当前用户有权限的命名空间；用户不具备集群级范围时 MUST NOT 提供"全部命名空间"选项，并 SHALL 自动选中首个可访问命名空间。

#### Scenario: 切换命名空间

- **WHEN** 用户在 Pod 列表页把命名空间从 `default` 切换为 `prod`
- **THEN** 列表 SHALL 立即按新命名空间加载，且地址栏可直接复制分享该视图

#### Scenario: 无权限的命名空间

- **WHEN** 用户切换到其无权限的命名空间
- **THEN** 界面 SHALL 展示明确的"无访问权限"状态，且不泄露该命名空间的资源数量

#### Scenario: 首次进入

- **WHEN** 只有命名空间级绑定的用户首次进入资源页且地址中没有命名空间
- **THEN** 界面 SHALL 自动选中首个可访问命名空间并加载其资源，MUST NOT 先发起全部命名空间请求

#### Scenario: 无集群级范围

- **WHEN** 用户只绑定到具体命名空间
- **THEN** 命名空间选择器 MUST NOT 出现"全部命名空间"，且集群级资源类型（如 Namespaces、Nodes）MUST NOT 出现在导航中