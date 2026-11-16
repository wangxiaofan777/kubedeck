## MODIFIED Requirements

### Requirement: 上下文导航

控制台 SHALL 以「集群 → 命名空间 → 资源类型 → 对象」为主导航层级，并通过面包屑与页面标题持续展示当前位置。用户 SHALL 能随时看到并切换当前上下文，切换后所有视图 MUST 跟随新上下文刷新。命名空间 SHALL 作为页面级上下文写入地址栏，切换时 MUST 给出明确的操作对象提示。命名空间选择器 SHALL 仅展示当前用户有权限的命名空间；用户不具备集群级范围时 MUST NOT 提供"全部命名空间"选项，并 SHALL 自动选中首个可访问命名空间。

#### Scenario: 切换命名空间

- **WHEN** 用户在 Pod 列表页把命名空间从 `default` 切换为 `prod`
- **THEN** 列表 SHALL 立即按新命名空间加载，面包屑与地址栏 SHALL 同步更新

#### Scenario: 在详情页切换命名空间

- **WHEN** 用户在某个 Pod 详情页切换命名空间
- **THEN** 界面 SHALL 明确提示将返回列表，MUST NOT 停留在旧对象上并静默改变上下文

#### Scenario: 无权限的命名空间

- **WHEN** 用户切换到其无权限的命名空间
- **THEN** 界面 SHALL 展示明确的"无访问权限"状态，且不泄露该命名空间的资源数量

#### Scenario: 首次进入

- **WHEN** 只有命名空间级绑定的用户首次进入资源页且地址中没有命名空间
- **THEN** 界面 SHALL 自动选中首个可访问命名空间并加载其资源，MUST NOT 先发起全部命名空间请求

#### Scenario: 无集群级范围

- **WHEN** 用户只绑定到具体命名空间
- **THEN** 命名空间选择器 MUST NOT 出现"全部命名空间"，且集群级资源类型（如 Namespaces、Nodes）MUST NOT 出现在导航中

### Requirement: 全局搜索

控制台 SHALL 提供全局命令面板（`Ctrl/⌘ + K`），支持按名称在允许的资源类型与命名空间范围内检索，并 SHALL 同时提供导航与动作入口。结果 MUST 区分「资源」「导航」「动作」三类，支持键盘上下选择与 Enter 打开。

#### Scenario: 搜索资源

- **WHEN** 用户输入 Pod 名称片段
- **THEN** 系统 SHALL 返回其有权访问的匹配对象，并可直接跳转详情

#### Scenario: 键盘操作

- **WHEN** 用户按下 `Ctrl/⌘ + K`
- **THEN** 命令面板 SHALL 打开并聚焦输入框；按 Esc SHALL 关闭

#### Scenario: 搜索请求失败

- **WHEN** 搜索过程发生网络或权限错误
- **THEN** 面板 SHALL 显示错误说明与重试入口，MUST NOT 显示成"没有匹配结果"

## ADDED Requirements

### Requirement: 页面层级与标题

每个视图 SHALL 具备可识别的页面标题与面包屑；浏览器标签标题 SHALL 随当前视图更新。任何任务 MUST NOT 只依赖堆积在同一列表页上的弹层来表达层级。

#### Scenario: 打开对象详情

- **WHEN** 用户从 Deployment 列表进入某个 Deployment
- **THEN** 面包屑 SHALL 展示「集群 / 命名空间 / Deployments / 对象名」，页面标题 SHALL 为对象名

#### Scenario: 直接打开深链接

- **WHEN** 用户直接访问对象详情链接
- **THEN** 面包屑与标题 SHALL 根据 URL 正确还原，MUST NOT 显示为列表页

### Requirement: 任务承载面

创建、编辑、批量删除、扩缩容、回滚等任务 SHALL 在独立页面或右侧 Drawer 中完成；MUST NOT 在列表页或详情页上叠加多层 Modal。危险操作确认 SHALL 展示受影响对象清单或影响范围。

#### Scenario: 新建资源

- **WHEN** 用户点击新建
- **THEN** 系统 SHALL 打开独立创建页面或宽 Drawer，提供表单与 YAML 两种模式

#### Scenario: 批量删除

- **WHEN** 用户选中多个对象并触发删除
- **THEN** 确认面 SHALL 列出全部对象名称与影响范围，用户确认后才提交

#### Scenario: 弹层嵌套

- **WHEN** 用户已打开一个任务 Drawer
- **THEN** 系统 MUST NOT 在其上再叠加新的 Drawer 或 Modal 作为主流程入口

### Requirement: 总览下钻

总览页的统计项 SHALL 可点击进入对应列表，并携带筛选条件。MUST NOT 存在只能查看、不能继续操作的汇总数字。

#### Scenario: 查看异常 Pod

- **WHEN** 用户点击总览中的 CrashLoop 数量
- **THEN** 系统 SHALL 跳转到 Pod 列表并应用 CrashLoop 筛选

#### Scenario: 查看节点

- **WHEN** 用户点击节点就绪数
- **THEN** 系统 SHALL 跳转到节点列表

### Requirement: 资源工作台

资源详情 SHALL 采用「主区 + 右侧上下文栏」布局；Owner 引用、节点、标签选择器匹配、关联工作负载 SHALL 可点击跳转。详情页 MUST 区分 403、404、集群不可达与一般加载失败。

#### Scenario: 从 Pod 跳到控制器

- **WHEN** 用户点击 Pod 的 Owner 引用 `ReplicaSet/web-abc`
- **THEN** 系统 SHALL 打开对应 ReplicaSet 详情或列表定位到该对象

#### Scenario: 查看节点上的 Pod

- **WHEN** 用户点击 Pod 所属 Node
- **THEN** 系统 SHALL 打开该节点详情，并展示该节点上的 Pod 列表

### Requirement: 批量操作栏

列表进入多选状态后 SHALL 展示批量操作栏，包含已选数量、清除选择与可用操作；批量操作 MUST 按对象携带命名空间，在网络全部命名空间视图下 MUST NOT 静默失败。

#### Scenario: 全部命名空间下批量删除

- **WHEN** 用户在"全部命名空间"视图选中多个不同命名空间的对象
- **THEN** 请求 SHALL 按对象携带命名空间，删除结果 SHALL 逐项返回成功或失败

### Requirement: 视觉与反馈一致性

控制台 SHALL 使用统一的设计 token（主色、字号、间距、圆角、表格密度、状态色）与统一的状态组件；状态展示 MUST 使用一致的成功/警告/危险语义，MUST NOT 在不同页面使用互相冲突的颜色规则。

#### Scenario: 状态颜色

- **WHEN** 同一资源在列表与详情中展示状态
- **THEN** 两处 SHALL 使用同一语义与颜色

#### Scenario: 操作反馈

- **WHEN** 写操作成功或失败
- **THEN** 界面 SHALL 给出与操作对象关联的反馈，并提供结果查看入口，而不只是一条全局 toast

### Requirement: 加载与刷新状态

首屏加载 SHALL 使用骨架屏；后台刷新 MUST NOT 遮罩已有内容。表格刷新时 SHALL 保留上次数据并显示轻量进度指示。

#### Scenario: 15 秒轮询

- **WHEN** 列表在后台自动刷新
- **THEN** 表格 MUST NOT 出现全屏 loading 遮罩或内容闪烁