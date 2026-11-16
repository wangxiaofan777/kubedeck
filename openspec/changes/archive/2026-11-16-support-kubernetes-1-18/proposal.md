## Why

目标环境的 apiserver 是 Kubernetes v1.18.16。当前平台默认最低版本为 1.34，接入会返回 400；Helm Chart 的 `kubeVersion` 也拒绝 1.18，导致该环境无法接入，也无法用 Helm 部署平台。1.18 与当前实现存在明确的 API 版本差异（`policy/v1`、`batch/v1` CronJob、`networking.k8s.io/v1` Ingress、PDB/HPA 版本），需要专门的 legacy 兼容层。

## What Changes

- 支持版本范围扩展为 **Kubernetes 1.18.0–1.37.x**；1.18 标记为 legacy（已 EOL），默认最低版本从 1.34 调整为 1.18。
- 已应用迁移不改动：新增迁移在设置仍为旧默认值 `"1.34"` 时更新为 `"1.18"`，不覆盖管理员自定义值。
- Pod 驱逐按集群实际服务版本选择 `policy/v1` 或 `policy/v1beta1`。
- 资源目录按集群发现结果解析遗留类型版本：CronJob 使用 `batch/v1beta1`，Ingress 使用 `networking.k8s.io/v1beta1`（当集群未提供 v1 时）。
- 引导式创建在 legacy 集群上生成对应版本的清单，包括 Ingress v1beta1 的旧 backend 结构。
- Helm Chart 放宽 `kubeVersion`，并按集群版本条件渲染 PDB（`policy/v1beta1`）、HPA（`autoscaling/v2beta2`）与 Ingress（`networking.k8s.io/v1beta1`）；1.18 不支持 `topologySpreadConstraints` 时给出明确失败。
- 增加可选的 1.18 集成验证与 HTTP 烟测路径，现有 1.34/1.37 行为保持不变。

## Capabilities

### New Capabilities

- `kubernetes-version-compat`: 版本范围与门禁、遗留 API 版本协商、资源目录版本解析、Helm 清单兼容与兼容性验证。

### Modified Capabilities

- `cluster-registry`: 「集群版本记录与支持边界」的最低支持版本改为 1.18，并补充 legacy 集群接入场景。
- `workload-control`: 「Pod 删除与驱逐」补充在仅提供 `policy/v1beta1` 的集群上自动协商并成功驱逐的场景。

## Impact

- 后端：`internal/cluster` 默认最低版本；`internal/kube` 驱逐版本协商；PG/MySQL 新增设置迁移；可选 1.18 集成测试。
- 前端：静态资源类型按发现结果解析 GVR；创建清单按有效 GVR 生成；相关查询缓存键补充版本。
- 部署：Helm `Chart.yaml`、PDB/HPA/Ingress/Deployment 模板与 values 注释。
- 文档：README、AGENTS、部署/离线/本地环境/发布说明与检查单。
- 不涉及：低于 1.18 的版本、Kubernetes 已移除 API 的自动转换、已 EOL 版本的安全背书。