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

### Requirement: 命名空间单一来源

涉及具体对象的操作 SHALL 只使用一个命名空间值。路径中的命名空间与查询参数不一致时，系统 MUST 返回 400 `INVALID_ARGUMENT`，MUST NOT 用其中一个做准入检查、用另一个执行操作。

#### Scenario: 参数不一致

- **WHEN** 用户请求 `/clusters/prod/pods/kube-system/foo/log?namespace=team-a`
- **THEN** 系统 SHALL 返回 400，且不向目标集群发起任何请求

#### Scenario: 平台绑定与实际对象一致

- **WHEN** 用户只绑定了 `team-a`，请求 `team-a` 下 Pod 的日志且未携带 query 参数
- **THEN** 平台准入与实际读取 SHALL 都作用于 `team-a`

### Requirement: 上游错误脱敏

返回给客户端的错误 MUST NOT 包含 apiserver 的原始响应、内部地址、RBAC 角色名或证书细节。系统 SHALL 返回稳定错误码与脱敏消息，并将原始错误写入服务端日志用于排障。

#### Scenario: apiserver 拒绝请求

- **WHEN** apiserver 返回 403 并带有内部角色信息
- **THEN** 客户端 SHALL 只收到 `FORBIDDEN` 与通用说明，原始内容 SHALL 只出现在服务端日志

#### Scenario: 集群不可达

- **WHEN** 连接目标集群失败
- **THEN** 客户端 SHALL 收到 `CLUSTER_UNREACHABLE`，消息 MUST NOT 包含内部 IP 或 DNS 细节

### Requirement: 节点视图

系统 SHALL 提供节点列表与节点详情，展示就绪状态、角色、版本、可分配 CPU/内存、Taints、Labels、Conditions 与该节点上的 Pod。节点列表 MUST 明确标识已停止调度的节点。

#### Scenario: 查看节点列表

- **WHEN** 用户打开节点页面
- **THEN** 列表 SHALL 展示每个节点的角色、状态、版本、可分配资源、Pod 数与是否可调度

#### Scenario: 查看节点上的 Pod

- **WHEN** 用户打开某节点详情
- **THEN** 系统 SHALL 展示该节点上的全部 Pod，并可跳转到 Pod 详情

#### Scenario: 指标不可用

- **WHEN** 集群没有 metrics API
- **THEN** 节点页面 SHALL 仍展示可分配资源与状态，并标注实际用量不可用

### Requirement: 节点调度控制

系统 SHALL 支持 cordon 与 uncordon，操作 MUST 经目标集群 apiserver 以当前用户身份鉴权，并写入审计。

#### Scenario: 停止调度

- **WHEN** 用户对节点执行 cordon
- **THEN** 节点 `spec.unschedulable` SHALL 变为 true，界面 SHALL 显示「已停止调度」

#### Scenario: 恢复调度

- **WHEN** 用户对已 cordon 的节点执行 uncordon
- **THEN** 节点 SHALL 恢复可调度，界面状态同步更新

#### Scenario: 无权限

- **WHEN** 用户没有节点 patch 权限
- **THEN** 系统 SHALL 返回 403，且不修改节点

### Requirement: 节点驱逐

系统 SHALL 支持 drain，并 MUST 默认跳过 DaemonSet 管理的 Pod 与镜像 Pod。对无控制器管理的 Pod 或使用 emptyDir 的 Pod，系统 MUST 默认阻止并要求显式确认。驱逐 MUST 受 PodDisruptionBudget 约束，失败项 SHALL 逐项返回原因，MUST NOT 因单个失败中断全部。

#### Scenario: 正常驱逐

- **WHEN** 用户对节点执行 drain 且所有 Pod 可安全驱逐
- **THEN** 系统 SHALL 逐 Pod 创建 Eviction 请求，并返回成功数量与逐项结果

#### Scenario: PDB 阻止

- **WHEN** 某 Pod 的 PDB 不允许继续中断
- **THEN** 该 Pod SHALL 标记为 blocked，其他 Pod SHALL 继续处理，响应 SHALL 包含 PDB 名称与允许中断数

#### Scenario: 未受控 Pod

- **WHEN** 节点上存在没有控制器 Owner 的 Pod 且未显式强制
- **THEN** 系统 SHALL 阻止该 Pod 的驱逐并说明原因

#### Scenario: 本地存储

- **WHEN** Pod 使用 emptyDir 且未显式允许删除本地数据
- **THEN** 系统 SHALL 阻止该 Pod 的驱逐并说明原因

#### Scenario: 操作审计

- **WHEN** drain 完成
- **THEN** 系统 SHALL 写入一条审计记录，包含节点、成功/跳过/失败数量与操作者

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
