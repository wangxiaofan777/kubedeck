# kubernetes-version-compat Specification

## Purpose
规定平台对 Kubernetes 版本范围的承诺、legacy 集群的 API 版本协商方式，以及部署清单在不同版本上的兼容行为，使用户可以在 1.18 到 1.37 的集群上获得一致的核心能力。

## Requirements

### Requirement: 支持的 Kubernetes 版本范围

系统 SHALL 支持 Kubernetes 1.18.0–1.37.x。默认最低支持版本 SHALL 为 1.18，并允许通过平台设置调整；低于当前最低版本的集群 MUST 被拒绝接入，错误信息 MUST 说明最低支持版本且 MUST NOT 创建集群记录。平台 MUST NOT 覆盖管理员已自定义的最低版本设置。

#### Scenario: 1.18 集群接入成功

- **WHEN** 管理员接入一个可连通的 Kubernetes 1.18.16 集群
- **THEN** 系统 SHALL 完成接入、记录并展示 `v1.18.16`，且不因版本门禁拒绝

#### Scenario: 低于 1.18 被拒绝

- **WHEN** 接入的集群版本为 1.17.x
- **THEN** 系统 SHALL 返回 400，错误信息说明最低支持版本为 1.18，且不创建集群记录

#### Scenario: 自定义最低版本

- **WHEN** 平台设置已把最低版本改为高于 1.18 的值
- **THEN** 系统 SHALL 按该值执行门禁，MUST NOT 被升级迁移覆盖

#### Scenario: 集群升级后的版本刷新

- **WHEN** 已接入的 1.18 集群升级到 1.22 或更高版本
- **THEN** 健康探测 SHALL 刷新展示版本，后续 API 访问 SHALL 按新版本可用的资源版本进行

### Requirement: 遗留 API 版本协商

对同一能力存在多个 API 版本的资源，系统 SHALL 依据目标集群实际服务的能力选择可用版本，MUST NOT 依赖用户手工配置 API 版本。协商 MUST 不改变 1.34+ 集群上原有的 API 版本选择。

#### Scenario: 1.18 上驱逐 Pod

- **WHEN** 目标集群只提供 `policy/v1beta1` Eviction
- **THEN** 系统 SHALL 使用该版本提交驱逐请求，并在 PDB 允许时成功驱逐
- **AND** 在 PDB 阻止时 SHALL 返回阻止原因

#### Scenario: 新版本集群上驱逐 Pod

- **WHEN** 目标集群提供 `policy/v1` Eviction
- **THEN** 系统 SHALL 继续使用 `policy/v1`，行为与升级前一致

#### Scenario: 1.18 上浏览与创建 CronJob/Ingress

- **WHEN** 目标集群只提供 `batch/v1beta1` CronJob 或 `networking.k8s.io/v1beta1` Ingress
- **THEN** 资源列表、详情、YAML 与实时更新 SHALL 使用该集群实际提供的版本
- **AND** 引导式创建 SHALL 生成该版本可提交的清单结构

#### Scenario: 新版本集群上的资源版本

- **WHEN** 目标集群提供 `batch/v1` CronJob 与 `networking.k8s.io/v1` Ingress
- **THEN** 系统 SHALL 保持使用 v1 版本，MUST NOT 降级到 beta 版本

### Requirement: 兼容的部署清单

平台 Helm Chart SHALL 允许安装到 Kubernetes 1.18+。对 1.18 不提供的资源版本，Chart SHALL 按集群版本渲染兼容版本；对 1.18 不支持的 Pod 字段，Chart SHALL 在显式配置时失败并说明原因，MUST NOT 静默生成无效清单。

#### Scenario: 在 1.18 上渲染

- **WHEN** 使用 `--kube-version 1.18.16` 渲染启用了 PDB、HPA 与 Ingress 的 Chart
- **THEN** 输出的 PDB SHALL 使用 `policy/v1beta1`，HPA SHALL 使用 `autoscaling/v2beta2`，Ingress SHALL 使用 `networking.k8s.io/v1beta1` 与旧 backend 结构

#### Scenario: 在新版本上渲染

- **WHEN** 使用 `--kube-version 1.37.0` 渲染同一 Chart
- **THEN** 输出 SHALL 保持 `policy/v1`、`autoscaling/v2` 与 `networking.k8s.io/v1`

#### Scenario: 1.18 上配置不支持的字段

- **WHEN** 在 1.18 上显式配置 `topologySpreadConstraints`
- **THEN** 渲染 SHALL 失败并指出该字段要求 Kubernetes 1.19+

### Requirement: 兼容性验证

遗留版本兼容性 MUST 通过真实 1.18 集群验证，覆盖发现、资源增删改查、watch、扩缩容与驱逐；现有 1.34/1.37 路径 MUST 保持回归通过。缺少 1.18 环境时本地测试 MAY 跳过，但发布检查 MUST 记录实际执行结果。

#### Scenario: 1.18 集成验证

- **WHEN** 设置真实 1.18 集群的测试 kubeconfig 并运行 legacy 集成用例
- **THEN** 发现、CRUD、watch、scale 与驱逐协商用例 SHALL 全部通过

#### Scenario: 新版本回归

- **WHEN** 运行现有 1.34/1.37 集成与单元测试
- **THEN** 所有用例 SHALL 保持通过，API 版本选择与升级前一致
