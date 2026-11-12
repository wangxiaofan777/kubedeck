# 发布说明

## 0.1.0 (2026-11-12)

KubeDeck MVP：多集群 Kubernetes 只读/受控写操作控制台。

### 功能

- OIDC 授权码 + PKCE 登录、服务端会话、IdP 撤销检测与首管理员引导
- 集群登记、AES-256-GCM 凭据加密、健康探测、版本校验（1.34–1.37）
- 任意 API 资源发现（容忍聚合 API 部分失败）、分页浏览、详情、事件
- Monaco YAML 编辑：schema 实时校验、Diff、dryRun、resourceVersion 冲突处理
- 工作负载伸缩、重启、历史/回滚、暂停/恢复、Pod 驱逐
- 日志跟随、暂停/恢复、多容器、上一实例、关键字过滤
- WebSocket 多路复用、增量推送、续传/resync、慢消费者与权限回收
- 异步审计、哈希链校验、查询与 CSV/JSON 导出
- 全局搜索、深链接、权限预检 `<Can>`、空态/错误态/降级态
- Helm Chart：Deployment/Service/Ingress/PDB/HPA/NetworkPolicy/ServiceMonitor
- Prometheus `/metrics` 与告警规则

### 兼容矩阵

| K8S | k3s | discovery+list | impersonate+SSAR | exec | 版本读取 |
|---|---|---|---|---|---|
| 1.37 | v1.37.2+k3s1 | ✅ 27 组，部分失败容忍触发 | ✅ 四断言全过 | ✅ | ✅ major=1 minor=37 |
| 1.34 | v1.34.13+k3s1 | ✅ 24 组，部分失败容忍触发 | ✅ 四断言全过 | ✅ | ✅ major=1 minor=34 |

`client-go` v0.37.2，Go 1.27。低于 1.34 的集群在接入时返回 400，不落库。

### 升级须知

- 数据库迁移自动执行且带 checksum；不要修改已执行的迁移文件。
- 会话保存在进程内存：多副本必须配置会话亲和，或使用单副本。
- 主密钥轮换需要保留上一版本，完成凭据重加密后再移除。
- 审计表默认保留 180 天，按需调整备份与归档。

### 已知限制

- 没有全局 informer 缓存；列表直连 apiserver 分页（每页最大 500）。
- 多副本会话依赖 Ingress 亲和，尚未使用 Redis。
- 集群登记目前通过管理 API，控制台暂未提供登记表单。
- 没有内置 exec 终端（设计预留，本期未实现）。

### 验证

- `make check`：后端 lint/vet/staticcheck/单测 + 前端 lint/typecheck
- `make test-db`：真实 PostgreSQL 集成测试
- `helm lint --strict`、`helm template`（默认与全可选组件）
- k3s 端到端：登录/接入/浏览/伸缩/日志/审计
- 最小权限：平台 SA 仅 impersonate；只读用户写请求 403
- 安全自查：CSRF/SSRF/XSS/凭据泄露/越权
- 压测：50k Pod/1000 Node 模拟，列表 P95 ≈ 1ms；WS 事件 < 2s