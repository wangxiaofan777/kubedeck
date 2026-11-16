## 1. 实时订阅恢复

- [x] 1.1 提取订阅帧构造与发送逻辑，`onopen` 与 `resync` 共用
- [x] 1.2 收到 `resync` 时清空资源版本、invalidate 查询并重新订阅；增加 5 秒内连续 resync 的关闭保护
- [x] 1.3 `useResourceRealtime` 接收并发送 `labelSelector`，加入 effect 依赖
- [x] 1.4 `ResourcesPage` 把当前标签选择器传给实时订阅
- [x] 1.5 为订阅帧/标签选择器逻辑补充前端单元测试

## 2. 权限预检动词

- [x] 2.1 `useCan` 增加 `subresource` 参数并透传
- [x] 2.2 新增平台动作到 RBAC 动词/子资源的纯函数映射
- [x] 2.3 `WorkloadActions` 使用映射：伸缩 `update`+`scale`、重启/回滚/暂停 `patch`
- [x] 2.4 Pod 驱逐改为 `create`+`eviction`
- [x] 2.5 为映射函数补充单元测试，覆盖只读用户与合法编辑者

## 3. 驱逐超时

- [x] 3.1 `isLongLivedRequest` 识别 `POST .../drain`
- [x] 3.2 补充中间件测试：普通请求仍受 30 秒超时约束，drain 不被截断
- [x] 3.3 核对 `DrainNode` 超时边界与 400 校验（上限 300 秒，超限返回 400）

## 4. 状态与上下文

- [x] 4.1 调整 `StatusPill` 规则顺序并补充 `statusTone` 测试
- [x] 4.2 集群接入成功后 invalidate `['clusters']`
- [x] 4.3 集群删除成功后 invalidate `['clusters']`
- [x] 4.4 `switchCluster` 固定跳转到新集群总览，丢弃旧对象路径与 query
- [ ] 4.5 多集群手动走查：接入后设置页、删除后列表、详情页切换集群（本机 dev 服务无数据库且仅注册一个集群，待有数据库的多集群环境补走查）

## 5. 测试闸门

- [x] 5.1 `TestMigrateIsIdempotent` 从嵌入迁移文件推导期望列表
- [x] 5.2 运行 `make test-db`，确认迁移测试通过
- [x] 5.3 运行 `go test ./cmd/... ./internal/...`
- [x] 5.4 运行 `npm run lint && npm run typecheck && npm run test && npm run build`
- [x] 5.5 运行 `openspec validate harden-console-reliability --strict`