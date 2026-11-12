# 发布检查单

每个版本发布前逐项确认并留档（填写执行人/时间/证据链接）。任一「必须」项不通过则不得发布。

## 1. 代码与规格

- [ ] `openspec validate add-kubedeck-mvp --strict` 通过。
- [ ] 行为变化先更新 `openspec/changes/<change>/specs/`，再改代码。
- [ ] `tasks.md` 勾选与实际验收证据一致，没有把未验证项标记完成。
- [ ] `make check` 通过（gofmt/vet/staticcheck/单测/前端 lint/typecheck）。
- [ ] `make test-db` 在真实 PostgreSQL 上通过（不静默跳过）。
- [ ] 前端 `npm run build` 成功；`go:embed all:dist` 有产物。
- [ ] `go mod tidy` 无未提交差异；依赖漏洞扫描无 critical/high。

## 2. 安全

- [ ] 没有 kubeconfig、token、私钥、主密钥、数据库密码进入仓库、日志或镜像。
- [ ] 平台 ServiceAccount 只绑定 `impersonate` users/groups/serviceaccounts。
- [ ] 只读用户绕过前端直接写：返回 403（手工验证或自动化用例）。
- [ ] CSRF：跨站 Origin / `Sec-Fetch-Site: cross-site` 被拒绝。
- [ ] SSRF：`http://` 与 `169.254.0.0/16` 登记被拒绝。
- [ ] CSP 响应头存在，且没有 `unsafe-eval`；生产 `style-src` 的 `unsafe-inline` 已记录为已知限制。
- [ ] `/metrics` 只在内网暴露。
- [ ] 生产 `cookie_insecure=false`，入口 HTTPS。
- [ ] `dev.enabled=false`，没有挂载开发 kubeconfig。
- [ ] Secret 的 K8s RBAC 已按命名空间最小化。

## 3. 数据库与迁移

- [ ] 新迁移只追加，不修改已执行文件（checksum 会阻止启动）。
- [ ] 迁移在空库与有存量数据的库上各执行一次。
- [ ] 备份已完成并验证可恢复。
- [ ] 审计分区覆盖未来 3 个月；保留策略符合合规要求。
- [ ] 回滚路径明确：镜像回滚是否兼容当前 schema；不兼容时从备份恢复并确认 RPO/RTO。

## 4. 部署

- [ ] `helm lint --strict` 通过。
- [ ] `helm template` 默认值与全可选组件（Ingress/PDB/HPA/NetworkPolicy/ServiceMonitor）均无报错。
- [ ] `kubectl apply --dry-run=server` 通过。
- [ ] 镜像 tag 不可变（digest 或唯一版本），没有使用 `latest`。
- [ ] 资源 requests/limits、PDB、HPA 与副本数符合容量评估。
- [ ] 多副本部署已配置会话亲和；否则保持单副本。
- [ ] NetworkPolicy 放行数据库、目标 apiserver 与 DNS。
- [ ] Ingress 超时支持 WebSocket（空闲超时 > 90s）。

## 5. 功能验收

- [ ] 登录 → 当前用户/集群范围 3 秒内返回。
- [ ] 登记一个 1.34 与一个 1.37 集群，版本读取正确。
- [ ] 浏览 Pod/Deployment/Service/CRD；部分 discovery 失败时其余资源仍可用。
- [ ] YAML 非法字段实时提示；Diff → dryRun → 提交成功；并发冲突返回 409。
- [ ] 伸缩 0→3→0 收敛；重启、回滚、暂停/恢复可用。
- [ ] 日志跟随/暂停/过滤/上一实例可用；超 5MB 截断提示。
- [ ] 实时推送 2 秒内到达；断线 30 秒内续传；权限回收后订阅停止。
- [ ] 审计查询/导出/哈希链校验通过；写请求不等待审计落库。
- [ ] 只读用户看不到写入口，直接调用写 API 返回 403。
- [ ] 集群不可达时其他集群仍可用，界面有降级提示。

## 6. 可观测性

- [ ] `/metrics` 可抓取，`kubedeck_cluster_up`、列表延迟、WS 连接数、审计队列有数据。
- [ ] 告警规则已加载，测试环境可触发至少一条并恢复。
- [ ] 日志为结构化 JSON，带 requestId；不记录请求体与凭据。
- [ ] 仪表盘包含 5xx、列表 P95、WS 连接/重同步、审计积压。

## 7. 发布与回滚演练

- [ ] 在预生产完成一次 `helm upgrade` 与一次 `helm rollback`。
- [ ] 回滚后登录、浏览、写操作、审计均正常。
- [ ] 发布窗口与值班人确认；变更记录包含镜像 digest、chart 版本、迁移列表。
- [ ] 发布后 30 分钟观察 5xx、P95、WS、审计队列与集群健康。

## 8. 留档

- 发布版本：
- Chart 版本：
- 镜像 digest：
- 迁移版本：
- 执行人/时间：
- 验收证据（链接）：
- 例外与风险接受人：