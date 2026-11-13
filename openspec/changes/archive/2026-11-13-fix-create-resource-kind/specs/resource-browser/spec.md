## ADDED Requirements

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