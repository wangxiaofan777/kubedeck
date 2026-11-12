# 告警与指标

KubeDeck 在 `/metrics` 暴露 Prometheus 文本格式指标。该端点不鉴权，必须只在内网暴露（NetworkPolicy 或 Ingress 白名单），不要把 `/metrics` 发布到公网。

## 指标

| 指标 | 类型 | 说明 |
|---|---|---|
| `kubedeck_http_requests_total{method,route,status}` | counter | 请求量，按路由模式与状态码 |
| `kubedeck_http_request_duration_seconds{method,route}` | histogram | 请求延迟，用于列表 P95 |
| `kubedeck_ws_connections` | gauge | 当前活跃 WebSocket 连接数 |
| `kubedeck_ws_resync_total{reason}` | counter | watch 断流、resourceVersion 过期、慢消费者导致的重同步 |
| `kubedeck_audit_queue_depth` | gauge | 审计异步队列待写条数 |
| `kubedeck_audit_retry_depth` | gauge | 等待补偿重试的审计条数 |
| `kubedeck_audit_dropped_total` | gauge | 队列溢出丢弃的累计条数（必须为 0） |
| `kubedeck_audit_degraded` | gauge | 1 表示审计写入处于降级 |
| `kubedeck_cluster_up{cluster}` | gauge | 最近一次健康探测，1 为 healthy |

## 规则

`deploy/prometheus/kubedeck-rules.yaml` 是 Prometheus Operator 的 `PrometheusRule`，包含：

- 集群连续 5 分钟不可达（critical）
- 审计积压/降级（warning）与审计丢弃（critical）
- watch 重同步频率异常（warning）
- 资源列表 P95 超过 300ms（warning）
- WebSocket 连接数相对 10 分钟基线异常（warning）
- 5xx 比例超过 1%（critical）

安装：

```bash
kubectl apply -f deploy/prometheus/kubedeck-rules.yaml
```

如果使用非 Operator Prometheus，把 `spec.groups` 内容合并到 `rule_files` 指向的 YAML。

## 验证

```bash
make test-rules
```

该目标会从 `PrometheusRule` 中抽取 `spec.groups`，用 `promtool check rules`
校验语法，并用 `promtool test rules deploy/prometheus/kubedeck-rules.test.yaml`
确认“集群不可达”和“审计积压”两条告警能在模拟序列上触发。依赖 `python3`（PyYAML）与 `promtool`。

## 验证规则可触发

在测试环境可临时降低阈值或使用以下方式制造信号：

- 集群不可达：登记一个指向不可路由地址的集群，等待健康探测。
- 审计积压：暂停 PostgreSQL 或限制其连接，观察 queue depth 与 degraded。
- 5xx：请求不存在的后端或注入故障。
- WS 重同步：重启 apiserver/watch 连接，使客户端收到 resync。

验证后恢复阈值，避免告警噪音。

## 说明

当前实现的实时订阅采用按订阅直连 watch，没有全局 informer 缓存；因此“同步延迟”用 `kubedeck_ws_resync_total` 与重同步频率来观测，而不是 informer `HasSynced` 延迟。