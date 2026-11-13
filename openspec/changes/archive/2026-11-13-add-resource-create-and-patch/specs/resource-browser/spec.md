## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: 只读用户限制

仅具备只读权限的用户 MUST NOT 看到创建、编辑、删除等写操作入口；即便看到，服务端也 MUST 拒绝其写请求。

#### Scenario: 只读用户界面

- **WHEN** `Viewer` 角色用户打开资源列表或资源详情
- **THEN** 界面 SHALL NOT 展示创建、编辑、删除按钮，且不发起任何写权限探测之外的请求

#### Scenario: 无创建权限时的新建入口

- **WHEN** 当前用户对当前资源类型没有 `create` 权限
- **THEN** 资源列表 SHALL NOT 展示"新建"入口