## ADDED Requirements

### Requirement: OIDC 身份声明校验

平台在建立登录会话前 SHALL 校验 ID Token 的身份声明。当 ID Token 携带非空 `email` 且未显式配置允许未验证邮箱时，`email_verified` MUST 为 `true`；缺失或为 `false` 时登录 MUST 失败，MUST NOT 建立会话或把该邮箱作为集群身份。

平台 MUST 在把 `groups` 声明用于会话与 Kubernetes impersonation 前完成规范化：去除首尾空白、丢弃空值、去重，并限制组数量与单个组名长度。规范化后的组 SHALL 保持首次出现顺序。

#### Scenario: 邮箱已验证

- **WHEN** IdP 返回 `email` 且 `email_verified=true`
- **THEN** 平台 SHALL 建立会话，并以该邮箱作为 `Impersonate-User`

#### Scenario: 邮箱明确未验证

- **WHEN** IdP 返回 `email` 且 `email_verified=false`
- **THEN** 登录 SHALL 失败并返回可定位的认证错误，MUST NOT 建立会话

#### Scenario: IdP 未返回验证状态

- **WHEN** IdP 返回 `email` 但没有 `email_verified` 声明
- **THEN** 默认配置下登录 SHALL 失败；仅当部署显式允许未验证邮箱时才 SHALL 继续

#### Scenario: 组声明包含空值与重复项

- **WHEN** IdP 的 `groups` 声明包含空白、空字符串或重复组名
- **THEN** 平台 SHALL 仅保留去空白、去重后的非空组名，并用于 `Impersonate-Group`

#### Scenario: 组声明超过上限

- **WHEN** IdP 返回的组数量或单个组名长度超过平台上限
- **THEN** 平台 SHALL 丢弃超出部分并记录告警，MUST NOT 把未经限制的声明发送到目标集群