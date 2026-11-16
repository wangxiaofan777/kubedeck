## 1. 版本门禁与迁移

- [x] 1.1 `internal/cluster/manager.go` 默认最低版本改为 1.18，并补/改单元测试验证 1.18 通过、1.17 拒绝
- [x] 1.2 新增 PG `0005` 与 MySQL `0002` 迁移：仅当设置为旧默认 `"1.34"` 时更新为 `"1.18"`；双库集成测试确认默认值变化且自定义值不被覆盖
- [x] 1.3 更新 `platform.min_k8s_version` 相关文档与前端/后端提示文案

## 2. 遗留 API 协商

- [x] 2.1 `DirectTransport.Evict` 探测并缓存 `policy/v1` 可用性，不可用时使用 `policy/v1beta1`；补单元测试覆盖两种选择
- [x] 2.2 将临时 1.18 spike 转为 `internal/kube/legacy_test.go`，由 `KUBEDECK_TEST_LEGACY_KUBECONFIG` 启用，覆盖 discovery、CRUD、watch、scale、eviction
- [x] 2.3 在真实 k3s v1.18.16 上运行 legacy 测试并记录结果

## 3. 前端版本解析与清单

- [x] 3.1 `resourceTypes.ts` 新增 `preferredResourceType` 与 `useResourceType`，按发现结果选择已服务版本；补单元测试
- [x] 3.2 `ResourcesPage`、`CreateResourceLayout`、`ResourceDetailLayout` 使用有效类型，查询键补充 GVR version
- [x] 3.3 `createManifest.ts` 新增 `buildManifestFor`：CronJob 使用有效版本，Ingress v1beta1 使用旧 backend 结构；`valuesFromResource` 兼容两种结构
- [x] 3.4 前端单测覆盖 1.18 发现结果、CronJob/Ingress legacy 清单与 v1 回归；`npm run lint/typecheck/test` 通过

## 4. Helm 兼容

- [x] 4.1 `Chart.yaml` 放宽 `kubeVersion` 到 `>=1.18.0-0`
- [x] 4.2 PDB/HPA/Ingress 按 `.Capabilities.KubeVersion.Version` 条件渲染；`topologySpreadConstraints` 在 <1.19 显式配置时失败
- [x] 4.3 `helm template --kube-version 1.18.16` 与 `1.37.0` 双版本渲染验证；`helm lint` 通过

## 5. 文档

- [x] 5.1 README/AGENTS/部署/离线/本地环境更新版本范围为 1.18–1.37，并标注 1.18 legacy、EOL 与 metrics-server 要求
- [x] 5.2 发布说明与检查单加入 1.18 legacy 集成测试为发布前必跑项

## 6. 验证

- [x] 6.1 `openspec validate support-kubernetes-1-18 --strict` 通过
- [x] 6.2 `gofmt`/`go vet`/`staticcheck`/`go build` 通过
- [x] 6.3 双库 `go test ./...` 通过，1.34/1.37 无回归
- [x] 6.4 1.18 HTTP 烟测：dev 模式接入 1.18，验证资源发现、ConfigMap 列表/创建、Deployment scale、Pod 驱逐、CronJob/Ingress legacy 版本（watch 由 2.3 的 legacy 集成测试覆盖）
- [x] 6.5 前端 lint/typecheck/test 与 Helm 双版本渲染通过