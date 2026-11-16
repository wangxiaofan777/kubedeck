## ADDED Requirements

### Requirement: 资源详情子视图路由

资源详情 SHALL 把概览、YAML 编辑、变更比较、事件、日志与关联 Pod 拆分为独立子视图，每个子视图 MUST 具备可分享的独立地址与页面标题。子视图之间 SHALL 通过详情页子导航切换，MUST NOT 依赖同一页面内的 Tabs 承载全部任务。

#### Scenario: 直接打开 YAML 子视图

- **WHEN** 用户直接访问某 Deployment 的 YAML 子视图地址
- **THEN** 系统 SHALL 加载该 Deployment 的 YAML 编辑页，面包屑与页面标题 SHALL 还原为「集群 / 命名空间 / Deployments / 对象名 / YAML」

#### Scenario: 从概览进入比较

- **WHEN** 用户在 YAML 子视图修改内容并触发比较
- **THEN** 系统 SHALL 进入独立的变更比较子视图，地址 SHALL 反映比较页，浏览器后退 SHALL 回到 YAML 编辑页

#### Scenario: 不适用子视图

- **WHEN** 当前资源不是 Pod
- **THEN** 详情子导航 MUST NOT 展示日志入口；当前资源没有可匹配的关联 Pod 时 MUST NOT 展示关联 Pod 入口

### Requirement: YAML 编辑与比较分离

YAML 编辑子视图 SHALL 只负责编辑与保存草稿；变更比较子视图 SHALL 负责展示原对象与草稿的 Diff、不可变字段提示、dryRun 与提交。用户未经过比较子视图时 MUST NOT 直接提交变更。

#### Scenario: 保存草稿并比较

- **WHEN** 用户在 YAML 编辑页点击“比较变更”
- **THEN** 系统 SHALL 保存当前草稿并进入比较页，比较页 SHALL 展示 Diff 与不可变字段提示

#### Scenario: 没有草稿直接打开比较页

- **WHEN** 用户直接访问比较页且本次编辑没有草稿
- **THEN** 系统 SHALL 展示“没有待比较的修改”状态，并提供返回 YAML 编辑页的入口，MUST NOT 提交任何内容

#### Scenario: 放弃草稿

- **WHEN** 用户从比较页返回编辑页并选择放弃修改
- **THEN** 系统 SHALL 清除本次草稿并恢复为服务端最新内容

### Requirement: 创建流程步骤化

新建资源 SHALL 拆分为配置、YAML 与确认三个步骤页；每个步骤 MUST 具备独立地址，用户 SHALL 能在步骤之间前进与后退。步骤之间 SHALL 保留已填写的表单与 YAML 草稿；关闭或完成创建后 SHALL 清除草稿。

#### Scenario: 从配置进入 YAML

- **WHEN** 用户在配置步骤填写名称与镜像后进入 YAML 步骤
- **THEN** YAML 步骤 SHALL 展示由表单生成的清单，用户 SHALL 能继续编辑

#### Scenario: 从 YAML 返回配置

- **WHEN** 用户在 YAML 步骤修改后返回配置步骤
- **THEN** 可映射字段 SHALL 回填到表单，无法映射的字段 SHALL 保留在草稿中并在确认步骤提示

#### Scenario: 确认后创建

- **WHEN** 用户在确认步骤提交
- **THEN** 系统 SHALL 先执行服务端 dryRun，通过后创建对象并跳转到新对象详情；创建成功后 MUST 清除本次草稿

#### Scenario: 刷新页面

- **WHEN** 用户在创建步骤中刷新浏览器
- **THEN** 系统 SHALL 从本次创建草稿恢复已填写内容；草稿不存在时 SHALL 从第一步重新开始

## MODIFIED Requirements

### Requirement: 任务承载面

创建、编辑、YAML 比较、批量删除、扩缩容、回滚等任务 SHALL 在独立页面中完成；MUST NOT 在列表页或详情页上叠加多层 Modal，也 MUST NOT 把多个互不相关的任务塞进同一页面的 Tabs。危险操作确认 SHALL 展示受影响对象清单或影响范围。

#### Scenario: 新建资源

- **WHEN** 用户点击新建
- **THEN** 系统 SHALL 打开独立创建流程，配置、YAML 与确认各自具备独立步骤页

#### Scenario: 编辑 YAML

- **WHEN** 用户从资源详情进入 YAML 编辑
- **THEN** 系统 SHALL 打开独立的 YAML 编辑页；比较与提交 SHALL 在独立的比较页完成

#### Scenario: 批量删除

- **WHEN** 用户选中多个对象并触发删除
- **THEN** 确认面 SHALL 列出全部对象名称与影响范围，用户确认后才提交

#### Scenario: 弹层嵌套

- **WHEN** 用户已打开一个任务 Drawer
- **THEN** 系统 MUST NOT 在其上再叠加新的 Drawer 或 Modal 作为主流程入口