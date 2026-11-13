# resource-browser Specification

## Purpose
提供对集群内任意 API 资源（含 CRD）的发现、浏览与维护能力，使用户无需记忆 kubectl 命令即可完成查看、编辑与删除，同时保证 YAML 变更可预览、可回溯。

## Requirements

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

仅具备只读权限的用户 MUST NOT 看到创建、编辑、删除等写操作入口；即便看到，服务端也 MUST 拒绝其写请求。

#### Scenario: 只读用户界面

- **WHEN** `Viewer` 角色用户打开资源列表或资源详情
- **THEN** 界面 SHALL NOT 展示创建、编辑、删除按钮，且不发起任何写权限探测之外的请求

#### Scenario: 无创建权限时的新建入口

- **WHEN** 当前用户对当前资源类型没有 `create` 权限
- **THEN** 资源列表 SHALL NOT 展示"新建"入口

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

### Requirement: 资源创建

系统 SHALL 支持通过资源集合接口创建任意已发现 API 资源（含 CRD）的对象，请求体 MUST 同时接受 YAML 与 JSON，SHALL 支持服务端 dry-run 校验。

#### Scenario: 创建成功

- **WHEN** 用户以合法 YAML 请求创建对象且具备 `create` 权限
- **THEN** 系统 SHALL 返回 201 与创建后的对象（含 `uid`、`resourceVersion`），并将本次操作以 `create` 动词写入审计

#### Scenario: 服务端校验

- **WHEN** 请求携带 `dryRun=All`
- **THEN** 系统 SHALL 让 apiserver 完成准入与校验并返回对象结果，但 MUST NOT 持久化该对象

#### Scenario: 名称冲突

- **WHEN** 目标命名空间中已存在同名对象
- **THEN** 系统 SHALL 返回 409 `CONFLICT`，MUST NOT 覆盖既有对象

#### Scenario: 命名空间约束

- **WHEN** 目标资源是命名空间级但请求未提供命名空间，或目标资源是集群级但请求提供了命名空间
- **THEN** 系统 SHALL 返回 400 `INVALID_ARGUMENT`，且不向上游发起创建请求

#### Scenario: 对象身份不一致

- **WHEN** 请求体中的 `apiVersion`、`kind` 与路径不一致，或 `metadata.name` 缺失/不合法，或 `metadata.namespace` 与查询参数不一致
- **THEN** 系统 SHALL 返回 422 `UNPROCESSABLE`，且不向上游发起创建请求

#### Scenario: 携带 resourceVersion

- **WHEN** 创建请求体包含非空 `metadata.resourceVersion`
- **THEN** 系统 SHALL 返回 422 `UNPROCESSABLE`，提示创建不得携带版本号

#### Scenario: 资源类型不支持创建

- **WHEN** 目标资源类型的发现结果不包含 `create` 动词
- **THEN** 系统 SHALL 返回 400 `UNSUPPORTED`，且不向上游发起创建请求

#### Scenario: 无创建权限

- **WHEN** 当前用户对目标资源没有 `create` 权限
- **THEN** 系统 SHALL 返回 403 `FORBIDDEN`，且不泄露对象内容

#### Scenario: 超大请求体

- **WHEN** 请求体超过配置上限（默认 1 MB）
- **THEN** 系统 SHALL 返回 413 `PAYLOAD_TOO_LARGE`

### Requirement: 部分更新

系统 SHALL 支持对已存在对象执行部分更新，MUST 根据请求媒体类型选择合并补丁、策略合并补丁或 Server-Side Apply；Server-Side Apply MUST NOT 静默覆盖由其他 field manager 管理的字段。

#### Scenario: 合并补丁

- **WHEN** 用户以 `application/merge-patch+json` 提交仅包含 `spec.replicas` 的补丁
- **THEN** 系统 SHALL 只修改该字段并返回 200 与更新后的完整对象

#### Scenario: 策略合并补丁

- **WHEN** 用户以 `application/strategic-merge-patch+json` 提交策略合并补丁
- **THEN** 系统 SHALL 按 Kubernetes 策略合并语义应用补丁并返回更新后的对象

#### Scenario: Server-Side Apply 字段冲突

- **WHEN** 用户以 `application/apply-patch+yaml` 应用对象，且某字段由其他 field manager 管理
- **THEN** 系统 SHALL 返回 409 `CONFLICT` 与冲突字段信息，MUST NOT 静默覆盖

#### Scenario: Server-Side Apply 强制接管

- **WHEN** 同一冲突请求携带 `force=true`
- **THEN** 系统 SHALL 完成字段接管并返回 200 与更新后的对象

#### Scenario: 缺少 field manager

- **WHEN** Server-Side Apply 请求未提供非空 `fieldManager`
- **THEN** 系统 SHALL 返回 400 `INVALID_ARGUMENT`

#### Scenario: 不支持的补丁类型

- **WHEN** 请求媒体类型不属于上述三种补丁类型
- **THEN** 系统 SHALL 返回 400 `UNSUPPORTED`，且不向上游发起请求

#### Scenario: 补丁 dry-run

- **WHEN** 补丁请求携带 `dryRun=All`
- **THEN** 系统 SHALL 返回校验后的对象且 MUST NOT 持久化变更

#### Scenario: 补丁身份不一致

- **WHEN** 补丁或 Apply 请求体中携带的 `metadata.name`、`metadata.namespace` 或 `apiVersion` 与请求路径不一致
- **THEN** 系统 SHALL 返回 422 `UNPROCESSABLE`

#### Scenario: 补丁审计

- **WHEN** 任意补丁请求完成
- **THEN** 系统 SHALL 以 `patch` 动词记录审计，且只保存请求体摘要哈希

### Requirement: 创建目标按 YAML 类型解析

创建弹窗 SHALL 以 YAML 中声明的 `apiVersion` 与 `kind` 作为实际创建目标，而不是固定使用打开弹窗时的资源页类型。系统 MUST 通过集群资源发现解析目标资源，并按其集合路径提交创建请求。

目标解析 MUST NOT 被用作授权依据；是否允许创建 MUST 仍由服务端按当前用户身份判定。解析结果只用于选择请求路径、校验命名空间规则与决定创建成功后的跳转。

#### Scenario: 跨类型创建

- **WHEN** 用户在 Deployments 页打开创建弹窗，把 YAML 改写为合法的 ConfigMap 并提交
- **THEN** 系统 SHALL 向 ConfigMap 的集合路径发起创建请求，成功后跳转到该 ConfigMap 的详情页，而不是返回路径与请求体不一致的错误

#### Scenario: 类型无法解析

- **WHEN** YAML 的 `apiVersion` 与 `kind` 在集群资源发现中不存在
- **THEN** 系统 SHALL 在客户端提示该类型未被集群发现，MUST NOT 发起创建请求

#### Scenario: 类型不支持创建

- **WHEN** 解析到的目标资源在发现结果中不包含 `create` 动词
- **THEN** 系统 SHALL 在客户端提示该类型不支持创建，MUST NOT 发起创建请求

#### Scenario: 集群级资源的命名空间冲突

- **WHEN** YAML 声明的目标资源是集群级，但 `metadata.namespace` 非空
- **THEN** 系统 SHALL 在客户端提示集群级资源不能设置命名空间，MUST NOT 发起创建请求

#### Scenario: 控制台无对应页面

- **WHEN** 目标类型创建成功，但控制台没有该类型的列表或详情页面
- **THEN** 系统 SHALL 提示创建结果并停留在当前页面，MUST NOT 猜测或跳转到不存在的路由

#### Scenario: 同类型创建保持原行为

- **WHEN** YAML 的 `kind` 与当前资源页类型一致
- **THEN** 系统 SHALL 按原有路径创建并跳转到该对象详情页

#### Scenario: 跨类型无权限

- **WHEN** 用户对 YAML 声明的目标类型没有 `create` 权限
- **THEN** 服务端 SHALL 返回 403，界面 SHALL 展示无权限错误，且不泄露对象内容
