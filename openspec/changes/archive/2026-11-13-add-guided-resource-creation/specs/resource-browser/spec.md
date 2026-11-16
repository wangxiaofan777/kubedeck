## ADDED Requirements

### Requirement: 引导式创建

系统 SHALL 为高频资源类型提供结构化创建表单，至少覆盖 Deployment、StatefulSet、Service、ConfigMap、Secret、CronJob、Ingress 与 Namespace。表单 MUST 与 YAML 视图双向同步，且 YAML MUST 作为高级模式保留。未知类型 SHALL 回退到 YAML 模式。

#### Scenario: 创建 Deployment

- **WHEN** 用户选择「Web 服务」模板并填写名称、镜像与副本数
- **THEN** 系统 SHALL 生成包含 selector、template、container、ports 与 resources 的合法 Deployment，并可在创建前预览 YAML

#### Scenario: 创建 Secret

- **WHEN** 用户以键值对输入 Secret 数据
- **THEN** 系统 SHALL 生成 `stringData`，MUST NOT 要求用户输入 base64

#### Scenario: 未知类型

- **WHEN** 用户选择没有表单的资源类型
- **THEN** 系统 SHALL 直接进入 YAML 模式，并说明该类型暂无表单

#### Scenario: 表单与 YAML 切换

- **WHEN** 用户在表单与 YAML 之间切换
- **THEN** 已填写且可映射的字段 MUST 保留，无法映射的字段 SHALL 保留在 YAML 中并提示

### Requirement: 创建校验与反馈

创建前系统 SHALL 执行本地校验与服务端 dryRun；服务端拒绝时 MUST 保留用户输入并展示可执行的错误说明。创建成功后 SHALL 进入新对象详情。

#### Scenario: 镜像为空

- **WHEN** 用户未填写镜像就提交
- **THEN** 本地校验 SHALL 阻止提交并聚焦到镜像字段

#### Scenario: 服务端拒绝

- **WHEN** apiserver 因名称冲突或准入策略拒绝创建
- **THEN** 界面 SHALL 保留表单内容，展示错误码与说明

#### Scenario: 创建成功

- **WHEN** 创建成功
- **THEN** 系统 SHALL 跳转到对象详情，并在返回列表时保留原筛选

### Requirement: 模板与复制

系统 SHALL 提供可运行的内置模板，并支持从现有工作负载复制为新对象。模板 MUST 通过同一表单模型生成，MUST NOT 维护独立的 YAML 文本副本。

#### Scenario: 从现有对象复制

- **WHEN** 用户在 Deployment 详情点击「复制为新」
- **THEN** 创建表单 SHALL 预填镜像、端口、环境变量与资源限制，名称 MUST 为空并要求用户输入

#### Scenario: 模板创建

- **WHEN** 用户选择「Web 服务」模板
- **THEN** 表单 SHALL 预填可运行的镜像、端口与副本数，用户只需确认名称与命名空间