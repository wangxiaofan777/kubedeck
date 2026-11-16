## Context

平台当前把最低版本固定为 1.34，并使用以下较新 API：

- Pod 驱逐固定 `policy/v1`（1.21+）。
- 前端静态资源目录把 CronJob 固定为 `batch/v1`（1.21+）、Ingress 固定为 `networking.k8s.io/v1`（1.19+）。
- Helm Chart 的 PDB/HPA/Ingress 分别为 `policy/v1`、`autoscaling/v2`、`networking.k8s.io/v1`，`kubeVersion >=1.34`。

对本地 k3s v1.18.16 的实测结果：client-go v0.37 的 discovery、动态 CRUD、watch、Deployment scale 均正常；实际失败点只有 `policy/v1` Eviction；集群只提供 `batch/v1beta1` CronJob、`networking.k8s.io/v1beta1` Ingress、`policy/v1beta1` PDB、`autoscaling/v1` HPA、`discovery.k8s.io/v1beta1` EndpointSlice。

## Goals / Non-Goals

**Goals:**

- 1.18.0–1.37.x 可接入，1.18 上核心路径（浏览、创建、YAML、扩缩容、日志、实时、驱逐）可用。
- 1.18 上可用 Helm 安装平台。
- 现有 1.34/1.37 行为与 API 版本不变。

**Non-Goals:**

- 不支持 1.18 以下版本。
- 不自动转换用户手写的旧 API YAML（例如把 `extensions/v1beta1` Deployment 转成 `apps/v1`）。
- 不为 EOL 版本提供安全背书；文档明确建议升级。

## Decisions

### D1. 版本门禁与默认值

`defaultMinVersion` 改为 `1.18`。数据库设置 `platform.min_k8s_version` 通过新迁移从旧默认 `"1.34"` 更新为 `"1.18"`，仅当值仍等于旧默认时执行，避免覆盖管理员自定义值。已应用迁移不改校验和。

### D2. Eviction 版本协商

`DirectTransport` 已有 discovery 客户端。`Evict` 在首次调用时探测 `policy/v1` 是否可用，结果缓存 5 分钟：

- 可用：body `apiVersion: policy/v1`（现状）。
- 不可用：body `apiVersion: policy/v1beta1`；请求路径仍为 `pods/{name}/eviction`，`deleteOptions` 结构相同。

不使用“先发 v1，失败再重试”的方式，避免把 PDB 429/403 与版本缺失混淆。

### D3. 资源目录版本解析

前端新增纯函数 `preferredResourceType(type, resources)`：

1. 发现结果中存在静态 GVR（group+version+kind）时直接使用。
2. 否则在同 group、同 resource/kind 的候选中选择已服务版本。
3. 同 group 无候选时才跨 group 选择（例如 Ingress 的 `extensions/v1beta1`）。

新增 `useResourceType(cluster, typeKey)` 基于现有 `useApiResources`，在列表页、创建页、详情布局中使用；详情子页面通过既有 context 继承有效类型。查询键补充 GVR version，避免发现结果到达后复用旧缓存。

### D4. 引导式创建清单

`buildManifestFor(type, values)` 用有效 GVR 生成 apiVersion：

- CronJob：`batch/v1beta1` 或 `batch/v1`，spec 结构相同。
- Ingress：v1 使用 `backend.service.name/port.number`；v1beta1 使用 `backend.serviceName/servicePort`，并保留 `pathType`（1.18 支持）。
- 其他类型继续由 GVR 生成（`apps/v1`、`v1` 等）。
- `valuesFromResource` 同时读取 Ingress 两种 backend 结构，保证复制功能在 legacy 集群可用。

### D5. Helm 清单兼容

按 `.Capabilities.KubeVersion.Version` 条件渲染：

| 资源 | < 版本阈值 | 现代版本 |
|---|---|---|
| PDB | `<1.21` → `policy/v1beta1` | `policy/v1` |
| HPA | `<1.23` → `autoscaling/v2beta2` | `autoscaling/v2` |
| Ingress | `<1.19` → `networking.k8s.io/v1beta1` + 旧 backend | `networking.k8s.io/v1` |
| topologySpreadConstraints | `<1.19` 且显式配置时 `fail` | 正常渲染 |

`Chart.yaml` 的 `kubeVersion` 放宽为 `>=1.18.0-0`。

### D6. 验证策略

- 单元测试覆盖版本解析、legacy 清单、Eviction 版本选择。
- `internal/kube/legacy_test.go` 读取 `KUBEDECK_TEST_LEGACY_KUBECONFIG`，在真实 1.18 上验证 discovery、CRUD、watch、scale、evict fallback；未配置时跳过。
- 1.18 HTTP 烟测使用 dev 模式启动平台，验证资源列表/创建、CronJob/Ingress legacy 版本、watch 与驱逐。
- 回归：双数据库测试、前端检查、Helm `--kube-version 1.18.16` 与 `1.37.0` 双渲染。

## Risks / Trade-offs

- **1.18 已 EOL** → 文档标注 legacy 并建议升级；不做安全背书。
- **client-go 0.37 对 1.18 超出官方 skew** → 只保证已覆盖的动态 CRUD/watch/log/scale/evict 主路径；SSA 等边缘行为不承诺。
- **不同 1.18 发行版差异** → 以发现结果为准做版本协商，避免硬编码单一发行版行为；实测 k3s 1.18.16。
- **legacy 类型缺少 v1 字段** → 创建清单使用 v1beta1 结构；用户手写 v1 YAML 仍会被 apiserver 拒绝，属于预期。
- **metrics** → 依赖兼容的 metrics-server（v1beta1）；未安装时沿用现有降级逻辑。
- **无 CI 1.18 集群** → legacy 集成测试作为发布检查单的本地必跑项，CI 继续覆盖 1.34/1.37。

## Migration Plan

1. 先落地门禁、迁移与 Eviction 协商，跑通现有双库与 1.37 回归。
2. 再改前端版本解析与清单，补单测。
3. 更新 Helm 与文档，1.18 渲染验证。
4. 回滚：代码回退到旧版本即可；新增迁移只把 `"1.34"` 改为 `"1.18"`，回退后仍可运行（旧代码接受 1.18 设置但不会主动降低，需手工恢复 `"1.34"`）。

## Open Questions

无。