## Context

见 `proposal.md — Why`。相关现状：

- `internal/auth/oidc.go` 的 `OIDCClaims` 已有 `EmailVerified bool`，但 `Provider.Exchange` 只校验 `sub`，未读取该字段；`Service.FinishLogin` 直接把邮箱写入用户与 `KubeUser`。
- `claims.Groups` 未做任何清洗，直接经 `identityFromUser` 进入 `Identity.Groups`，再由 `UserRef()` 传给 client-go 的 impersonation 头。
- `internal/auth/sessions.go` 明确说明会话与访问令牌保存在进程内，多副本需要共享存储；但 `deploy/helm/kubedeck/values.yaml` 的 `replicaCount` 没有任何约束，模板会如实渲染。

## Goals / Non-Goals

**Goals:**

- 默认拒绝 IdP 明确标记为未验证的邮箱，避免把未经确认的邮箱当作集群身份。
- 让 `groups` 声明进入 impersonation 前是确定、有限、可预测的。
- 让部署清单在会话实现不支持多副本时直接失败，而不是运行时随机 401。
- 用测试与文档固定这些约束。

**Non-Goals:**

- 不实现 Redis 或其他共享会话存储；`SessionStore` 扩展点保持不变。
- 不新增 IdP 端配置接口或管理页面。
- 不改变平台角色绑定、命名空间范围或 Kubernetes RBAC 判定逻辑。
- 不把组声明映射为平台角色；平台角色仍由 `role_bindings` 决定。

## Decisions

### 1. 邮箱验证采用“默认严格、显式放宽”

`OIDCConfig` 增加 `AllowUnverifiedEmail bool`，配置项为 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL`，默认 `false`。校验规则：

- 当 ID Token 含非空 `email` 且未允许未验证邮箱时，`email_verified` 必须显式为 `true`；
- 缺失 `email` 时不做邮箱校验（身份退回 `sub`，与现有行为一致）；
- 允许未验证邮箱时只记录告警日志，不阻断登录。

`OIDCClaims.EmailVerified` 改为 `*bool`，以区分“IdP 未提供”和“IdP 明确返回 false”。默认严格模式下两者都会被拒绝，但错误信息可以区分，便于排障。

替代方案：始终强制 `email_verified`。会直接排除不返回该声明的 IdP，且没有迁移路径；显式开关更可控。

### 2. 组声明规范化

新增纯函数 `normalizeGroups([]string) []string`：

- 去除首尾空白；
- 丢弃空字符串；
- 去重且保持首次出现顺序；
- 最多保留 64 个组，单个组名最多 256 字符（超出部分丢弃并记录一次告警）。

在 `Exchange` 返回前应用，`UserInfo` 同样应用，保证所有入口一致。规范化是防御性边界，不改变合法 IdP 的语义。

替代方案：只依赖 IdP 保证声明质量。IdP 配置错误或恶意租户可能产生超大/重复组，平台不应把未经检查的值直接放大到每个集群请求。

### 3. Helm 在渲染期拒绝多副本

`deployment.yaml` 开头增加：

```gotemplate
{{- if gt (int .Values.replicaCount) 1 }}
{{- fail "KubeDeck sessions are stored in memory; replicaCount must be 1 until a shared session store is implemented" }}
{{- end }}
```

`values.yaml` 的 `replicaCount` 注释写明约束。这样 `helm template`/`helm install` 在配置阶段失败，而不是上线后出现随机登出。

替代方案：在文档中提醒。容易被忽略，且 `helm upgrade --set replicaCount=2` 仍会静默破坏登录。

### 4. 配置与文档

- `internal/config` 增加字段、环境变量解析与默认值；不改变现有配置校验的必填项。
- `docs/deployment.md` 增加“IdP 声明要求”和“单副本约束”小节，说明 `email_verified` 缺失时的处理与关闭校验的风险。
- `deploy/compose/README.md` 同步单副本说明（Compose 本身固定单实例）。

## Risks / Trade-offs

- [默认严格会让部分 IdP 登录失败] → 错误信息明确指出 `email_verified`，文档给出 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true` 的显式放宽方式；这是安全默认值的必要代价。
- [限制组数量可能丢弃合法的大规模组成员关系] → 64 个组已覆盖常见企业目录；超限会记录告警，运营可发现并调整 IdP 声明。
- [Helm 渲染期失败会阻断已有用户升级] → 只有显式设置多副本才会触发；默认值仍为 1，升级路径不受影响。
- [不实现共享会话，横向扩展仍不可用] → 本次只把约束显式化，避免“看似支持多副本、实际随机 401”；共享存储留作独立变更。

## Migration Plan

1. 发布前确认 IdP 的 ID Token 是否包含 `email_verified=true` 与 `groups`。
2. 若 IdP 不返回 `email_verified`，在部署环境设置 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL=true`，并在安全评审中记录原因。
3. Helm 用户保持 `replicaCount: 1`；需要多副本时先实现共享会话存储。
4. 回滚：配置项与声明校验均为局部改动，可回滚到旧镜像；无数据库迁移。

## Open Questions

- 是否需要在指标端点暴露“因邮箱未验证被拒绝的登录次数”？本期先记录日志，等有观测需求再加。