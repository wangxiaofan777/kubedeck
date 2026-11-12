# 运维手册

面向值班与平台运维：日常健康检查、升级、回滚与常见故障处理。部署步骤见 [deployment.md](./deployment.md)。

## 1. 日常检查

```bash
kubectl -n kubedeck-system get pods
kubectl -n kubedeck-system rollout status deploy/kubedeck
curl -fsS https://kubedeck.example.com/healthz
curl -fsS https://kubedeck.example.com/readyz
```

`/healthz` 只表示进程存活；`/readyz` 会检查 PostgreSQL，失败返回 503。`/metrics` 用于 Prometheus 抓取。

日志：

```bash
kubectl -n kubedeck-system logs deploy/kubedeck --tail=200
kubectl -n kubedeck-system logs deploy/kubedeck --tail=200 | grep requestId
```

错误响应都带 `requestId`，用它关联日志与审计。

## 2. 升级

1. 阅读 release notes 与数据库迁移说明。
2. 备份 PostgreSQL（见 [backup-restore.md](./backup-restore.md)）。
3. `helm upgrade kubedeck deploy/helm/kubedeck -n kubedeck-system -f values-prod.yaml --wait --timeout 5m`
4. 检查 `kubectl rollout status`、`/readyz`、集群列表与一次只读浏览。
5. 观察 5xx、列表 P95、WS 重同步与审计队列 10 分钟。

迁移带 checksum，已执行迁移文件被修改会导致启动失败；不要手改迁移文件。

## 3. 回滚

```bash
helm history kubedeck -n kubedeck-system
helm rollback kubedeck <revision> -n kubedeck-system --wait --timeout 5m
```

镜像回滚不会自动回滚数据库。跨版本回滚前确认旧代码兼容当前 schema；不兼容时从备份恢复，并接受回滚点之后的数据丢失。

## 4. 常见故障

### 4.1 Pod CrashLoopBackOff

```bash
kubectl -n kubedeck-system logs deploy/kubedeck --previous --tail=200
```

常见原因：

- `master_key` 缺失或不是 base64 的 32 字节。
- `database_url` 错误或数据库不可达。
- OIDC discovery 失败：检查 issuer 可达、证书与 redirect URL。
- 迁移 checksum 不匹配：恢复原迁移文件或从备份恢复。

### 4.2 `/readyz` 503

数据库不可达。检查 Secret、网络策略、PostgreSQL 连接数。读路径在数据库故障时仍可用，但登录、角色绑定与审计写入会受影响；写操作应返回 503，不要绕过。

### 4.3 登录 401/重定向循环

- IdP 回调地址必须与 `KUBEDECK_OIDC_REDIRECT_URL` 完全一致。
- 生产必须 HTTPS，`cookie_insecure=false`。
- 检查浏览器是否拦截 Secure/HttpOnly Cookie。
- IdP 禁用用户后，下一个会话校验周期（≤5 分钟）返回 401。

### 4.4 集群不可达

```bash
kubectl -n kubedeck-system logs deploy/kubedeck --tail=200 | grep -i 'cluster\|probe'
```

检查登记的服务地址、平台 SA 凭据是否过期、目标 apiserver 证书、NetworkPolicy 出站 6443。不要把目标集群的 `cluster-admin` kubeconfig 作为修复手段；平台 SA 只需要 impersonate。

### 4.5 403 但用户认为自己有权限

1. 用目标集群验证用户 RBAC：`kubectl auth can-i get pods -n <ns> --as=<user>`。
2. 检查平台角色绑定是否覆盖该集群/命名空间（`/api/v1/admin/role-bindings`）。
3. 检查用户组 claim 是否包含预期 group；Impersonate-Group 由 IdP 决定。
4. `/api/v1/auth/can` 结果缓存最多 30 秒，等待后重试。

### 4.6 WebSocket 频繁断开/重同步

- 检查 Ingress/代理的超时与 `Upgrade` 头；心跳周期 30 秒，读超时 90 秒，代理空闲超时应大于 90 秒。
- 检查 `kubedeck_ws_resync_total` 的 reason。
- 单用户最多 5 个连接；超出会返回 429，关闭闲置页面。
- 权限回收后订阅会在 60 秒内终止。

### 4.7 审计链校验失败

```bash
curl -b 'kubedeck_session=<cookie>' \
  'https://kubedeck.example.com/api/v1/admin/audit-logs/verify?cluster=<cluster>'
```

返回首个断裂点。审计表只追加，不应有应用侧更新/删除；若发现篡改，冻结相关账号、保留数据库快照并走安全事件流程。

### 4.8 日志流中断

- 检查 Pod 是否重启/终止；`previous=true` 查看上一实例。
- 单次上限 5 MB，超出会截断并返回提示。
- 代理需允许 chunked 响应，关闭响应缓冲。

## 5. 容量与扩缩容

- 单实例默认 5 个 WS 连接/用户、单连接 20 个订阅；连接数受内存与 apiserver watch 数限制。
- 列表每页 500，服务端最大 500；大集群优先用命名空间/标签过滤。
- HPA 需要 metrics-server；默认按 CPU 70% 扩容。
- 多副本时平台会话保存在进程内存，**不支持无粘性负载均衡**。生产多副本必须在 Ingress 开启会话亲和，或将会话迁移到 Redis（当前版本未实现）。单副本部署最稳妥。
- 审计表按月分区，默认保留 180 天；扩容前确认磁盘与备份窗口。

## 6. 安全事件处置

1. 立即轮换 OIDC client secret、数据库密码与主密钥（见 deployment.md 轮换步骤）。
2. 删除可疑角色绑定，禁用账号（`POST /api/v1/admin/users/{id}/status`）。
3. 导出审计日志并校验哈希链。
4. 检查目标集群中平台 SA 的 ClusterRoleBinding 是否被扩大到 `cluster-admin`。
5. 保留 Pod 日志、数据库快照与 requestId 清单。