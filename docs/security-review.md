# 安全自查

范围：KubeDeck 平台自身（认证、授权、请求处理、凭据、审计、前端）。目标集群的 RBAC 与网络策略由集群管理员负责。

执行时间：2026-11-12；环境：k3s v1.37.2 + Helm 部署 + 平台 SA 最小权限。

## 1. 检查结果

| 项目 | 方法 | 结果 |
|---|---|---|
| CSRF | 带 `Origin: https://evil.example` 与 `Sec-Fetch-Site: cross-site` 的写请求 | 403 `FORBIDDEN`，请求未执行 |
| XSS | 源码检查 `dangerouslySetInnerHTML`/`innerHTML`/`eval`；CSP 响应头 | 业务源码无注入点；CSP `script-src 'self'`，`object-src 'none'`，`frame-ancestors 'none'`，无 `unsafe-eval` |
| SSRF | 登记集群时提交 `http://169.254.169.254` 与 `http://example.com` | 400 `INVALID_ARGUMENT`，未发起连接、未落库 |
| 越权（IDOR/RBAC 绕过） | 只读用户 `alice` 直接调用删除 Pod | 403 `FORBIDDEN`；`/auth/can` 返回 get=true、delete=false |
| 平台 SA 权限 | `kubectl auth can-i` + 代请求 | SA 不能 list/create Pod；只能 impersonate users/groups/serviceaccounts |
| 凭据泄露 | `GET /clusters/{c}/credential` 响应与审计请求体 | 仅返回 `authType/fingerprint/keyVersion/rotatedAt/expiresAt`，无 token/私钥/CA |
| 审计完整性 | 哈希链校验接口 | 通过，能定位首个断裂点（单测覆盖） |
| 会话 Cookie | 响应头与代码 | `HttpOnly`、`SameSite=Lax`、生产 `Secure`；不在 URL/前端存储 |
| 错误信息 | 错误响应 | 统一 `{code,message,details,requestId}`；不返回堆栈或上游凭据 |
| SQL 注入 | 仓储代码检查 | 全部参数化查询，无字符串拼接用户输入 |
| 依赖 | Dependabot（Go/npm/Actions/Docker） | 已配置，持续更新 |

## 2. 已验证的攻击面

### CSRF

写请求使用 Cookie 会话，因此：

- `sameOrigin` 中间件拒绝跨站 `Origin` 与 `Sec-Fetch-Site: cross-site`。
- Cookie `SameSite=Lax` 阻止跨站 POST 携带会话。
- CORS 白名单不允许 `*`，且不反射任意 Origin。

### SSRF

集群登记只允许 `https://`，拒绝 `169.254.0.0/16`（云元数据地址）。仍允许 RFC1918 私网地址，这是管理私有集群的业务需要；登记接口仅平台管理员可用。不要把登记接口暴露给非管理员。

### XSS

- React 默认转义所有文本；没有 `dangerouslySetInnerHTML`。
- CSP 禁止内联脚本与外部脚本，只允许同源。
- Ant Design 5 运行时注入样式，`style-src` 必须包含 `'unsafe-inline'`；这是已知的样式层放宽，不允许脚本执行。
- Monaco/YAML 内容作为编辑器文本处理，不注入 DOM。

### 权限提升

- 目标集群平台 SA 只有 impersonate；即使平台进程被攻破，也不能直接列取资源。
- 所有资源请求使用 `Impersonate-User`/`Impersonate-Group`，apiserver 做最终判定。
- 前端 `<Can>` 只影响展示；服务端与 apiserver 双重校验。
- 平台管理员不会自动获得目标集群权限；仍需目标集群 RBAC 绑定。

### 凭据

- AES-256-GCM 加密后存 PostgreSQL；主密钥只在进程内存与 Secret 中。
- 凭据接口只返回元数据；日志不打印 kubeconfig/token。
- 审计只保存请求体 SHA-256 摘要，不保存明文。
- Secret 的 YAML 视图会显示明文（这是功能），因此 Secret 的 `get` 权限必须严格控制。

## 3. 残余风险与建议

| 风险 | 影响 | 建议 |
|---|---|---|
| 会话在进程内存 | 多副本且无会话亲和时登录态不稳定 | 单副本，或 Ingress 会话亲和；后续迁移 Redis |
| `/metrics` 未鉴权 | 暴露请求量、集群名、延迟 | 只在内网暴露，NetworkPolicy 限制抓取来源 |
| `style-src 'unsafe-inline'` | 样式注入面 | 升级到支持 nonce 的方案后收紧 |
| 私网 SSRF | 管理员可登记任意私网地址 | 仅平台管理员可登记；审计所有登记/轮换 |
| Secret 明文展示 | 有权限用户可读取明文 | 按命名空间授予 Secret get；审计敏感读 |
| 审计异步队列满会丢弃 | 合规风险 | 监控 `kubedeck_audit_dropped_total`，容量规划；队列满时告警为 critical |
| dev 模式无认证 | 完全绕过登录 | 仅本地使用，禁止暴露网络；chart 默认关闭并在 NOTES 警告 |

## 4. 上线前必须确认

- [ ] `cookie_insecure=false`，入口 HTTPS。
- [ ] `dev.enabled=false`。
- [ ] 主密钥来自 Vault/KMS，不在 values/镜像/CI 日志中。
- [ ] 目标集群平台 SA 只绑定 impersonate ClusterRole。
- [ ] `/metrics` 未暴露公网。
- [ ] 数据库不对公网开放，使用 TLS。
- [ ] 审计备份与哈希链校验已演练。