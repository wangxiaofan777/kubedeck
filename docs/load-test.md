# 压测记录

目标：验证列表 P95 < 300ms、WebSocket 事件端到端 < 2s、内存符合预期。

## 1. 方法

### 列表

`internal/api/load_test.go` 的 `TestScaleListP95`：

- 构造 1000 个 Node 与 50,000 个 Pod 的 metadata-only 对象。
- 模拟 apiserver 的 `limit/continue`：每次只返回一页 500 条。
- 通过完整 chi 路由、鉴权中间件与 JSON 序列化执行 100 次
  `GET /api/v1/clusters/prod/resources/_/v1/pods?namespace=ns-1&limit=500`。
- 取第 95 百分位。

这覆盖 Kubedeck 自身处理与序列化一页的成本；apiserver 对 50k 对象的筛选与分页不在范围内，生产环境的页大小上限为 500。

### WebSocket

`TestStreamSubscribeAndIncrementalEvent` 在注入 ADDED 事件后读取客户端帧，断言端到端延迟 ≤ 2 秒。测试使用真实 gorilla WebSocket 连接与路由。

### 内存

- 模拟对象堆占用由 `runtime.ReadMemStats` 记录。
- 部署实例空闲内存由 `kubectl top pod` 记录。

## 2. 结果（2026-11-12，WSL2 / k3s v1.37.2）

```text
simulated objects heap=55 MiB
pods=50000 nodes=1000 page=500 runs=100 p95=992.045µs max=1.097699ms
PASS TestStreamSubscribeAndIncrementalEvent (0.00s)
```

部署实例：

```text
kubedeck-...  1m CPU  10Mi memory
```

结论：

- 列表 P95 ≈ 1ms，远低于 300ms；即使加入 apiserver 延迟也有充足余量。
- WS 事件在本机测试中毫秒级，满足 2s 目标。
- 50k 对象若全部驻留内存约 55MiB（metadata-only），但当前实现按页直连，不常驻 50k 对象；部署实例空闲 10MiB，限额 512MiB 足够。

## 3. 限制与后续

- 没有在真实 1000 节点/50k Pod 集群上执行，因为本地 k3s 无法承载该规模；结果用于验证平台侧成本上界。
- 列表直连 apiserver 分页，不依赖 informer 缓存；大规模集群的 P95 主要受 apiserver 分页与网络影响，后续可在需要时引入按用户隔离的缓存。
- 未压测多副本与 100+ 并发用户；建议在预生产用 `k6`/`hey` 对真实集群补测，并观察 `kubedeck_http_request_duration_seconds` 与 `kubedeck_ws_connections`。

## 4. 复现

```bash
cd /mnt/d/AI_Coding/kubedeck
go test ./internal/api -run TestScaleListP95 -v
go test ./internal/api -run TestStreamSubscribeAndIncrementalEvent -v
kubectl -n kubedeck-system top pod
```