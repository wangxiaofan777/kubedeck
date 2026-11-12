## Purpose

提供对集群内任意 API 资源（含 CRD）的发现、浏览与维护能力，使用户无需记忆 kubectl 命令即可完成查看、编辑与删除，同时保证 YAML 变更可预览、可回溯。

## ADDED Requirements

### Requirement: 资源发现

系统 SHALL 从目标集群发现全部可用的 API 资源，包括内置资源与 CRD，并在某个 API 组不可用时继续提供其余资源。

#### Scenario: 部分 API 组故障

- **WHEN** 集群中 `metrics.k8s.io` 等聚合 API 暂时不可用
- **THEN** 系统 SHALL 仍返回其余资源列表，并在界面上标注该 API 组当前不可用，而不是整体失败

#### Scenario: CRD 可见

- **WHEN** 集群中安装了新的 CRD
- **THEN** 系统 SHALL 在资源目录中出现该资源，并在一个发现周期内可正常浏览其实例

### Requirement: 资源列表

系统 SHALL 支持按集群、命名空间、资源类型列出对象，并支持标签选择、字段选择、分页与排序。

#### Scenario: 分页浏览

- **WHEN** 某命名空间内 Pod 数量超过单页上限
- **THEN** 系统 SHALL 返回分页游标，客户端可继续获取后续数据，且不重复、不遗漏

#### Scenario: 按状态过滤

- **WHEN** 用户筛选处于 `CrashLoopBackOff` 的 Pod
- **THEN** 列表 SHALL 仅返回匹配该状态的对象，并显示匹配数量

### Requirement: 资源详情

系统 SHALL 展示单个对象的完整元数据、状态、所属关系与最近事件。

#### Scenario: 查看 Pod 详情

- **WHEN** 用户打开一个 Pod
- **THEN** 系统 SHALL 展示其容器状态、重启次数、所在节点、Owner 引用与最近事件

### Requirement: YAML 编辑与提交

系统 SHALL 在提交前展示变更 Diff 并执行结构校验；提交冲突时 MUST 返回 409 并要求用户刷新，MUST NOT 静默覆盖他人变更。

#### Scenario: 提交合法变更

- **WHEN** 用户修改 Deployment 的镜像并提交
- **THEN** 系统 SHALL 先展示原对象与新对象的 Diff，确认后提交，并返回新的 `resourceVersion`

#### Scenario: 并发冲突

- **WHEN** 用户提交的对象 `resourceVersion` 已过期
- **THEN** 系统 SHALL 返回 409 与最新版本信息，不覆盖集群中的现有对象

#### Scenario: 不可变字段

- **WHEN** 用户尝试修改 Deployment 的 `spec.selector`
- **THEN** 系统 SHALL 在提交前提示该字段不可变，而不是提交后返回服务端错误

### Requirement: 删除与批量操作

删除 SHALL 要求二次确认并支持传播策略；批量删除 MUST 展示受影响对象的完整清单。

#### Scenario: 批量删除

- **WHEN** 用户选择 5 个 Pod 执行批量删除
- **THEN** 系统 SHALL 展示这 5 个对象的名称与命名空间，确认后逐个执行并汇总每项结果

#### Scenario: 部分失败

- **WHEN** 批量操作中某个对象因权限或终态保护失败
- **THEN** 系统 SHALL 继续处理其余对象，并在结果中逐项标明成功或失败原因

### Requirement: 只读用户限制

仅具备只读权限的用户 MUST NOT 看到编辑、删除等写操作入口；即便看到，服务端也 MUST 拒绝其写请求。

#### Scenario: 只读用户界面

- **WHEN** `Viewer` 角色用户打开资源详情
- **THEN** 界面 SHALL 不展示编辑、删除按钮，且不发起任何写权限探测之外的请求
### Requirement: 异常输入与容量边界

系统 MUST 对非法 GVR、非法对象名与超大载荷返回明确错误，MUST NOT 将其透传为 5xx。

#### Scenario: 非法 GVR

- **WHEN** 请求中的 group/version/resource 不在发现结果中
- **THEN** 系统 SHALL 返回 400 `INVALID_ARGUMENT`，并提示可用资源类型

#### Scenario: 路径穿越尝试

- **WHEN** 对象名包含 `/`、`..` 或其他路径控制字符
- **THEN** 系统 SHALL 拒绝请求，不拼接进上游 URL

#### Scenario: 超大 YAML

- **WHEN** 提交的 YAML 超过配置上限（默认 1 MB）
- **THEN** 系统 SHALL 返回 413，并提示上限值

#### Scenario: 空列表

- **WHEN** 查询条件匹配不到任何对象
- **THEN** 系统 SHALL 返回空数组与 `remainingItemCount=0`，而不是 404
