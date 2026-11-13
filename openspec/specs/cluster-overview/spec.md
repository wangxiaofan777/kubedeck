# cluster-overview Specification

## Purpose
为控制台提供单集群的只读总览，让用户在进入集群时一眼看到节点健康、可分配容量、Pod 状态分布、异常工作负载与最近事件，同时保证统计口径严格收敛在当前用户的授权范围内。

## Requirements

### Requirement: 总览聚合数据

系统 SHALL 提供单集群总览读接口 `GET /api/v1/clusters/{cluster}/overview`，一次返回以下信息：节点总数与就绪数、节点可分配 CPU 与内存汇总、命名空间数量、Pod 状态分布（Running/Pending/Succeeded/Failed/Unknown）、异常工作负载计数（Deployment、StatefulSet、DaemonSet）以及最近事件摘要。

统计数据 MUST 实时从集群读取，MUST NOT 写入平台数据库；接口 MUST 返回统计生成时间，使前端能展示数据新鲜度。

#### Scenario: 正常返回总览

- **WHEN** 用户请求其有权访问集群的总览
- **THEN** 系统 SHALL 返回节点、容量、命名空间、Pod 状态、工作负载异常与最近事件的计数及生成时间

#### Scenario: 空集群

- **WHEN** 集群可访问但没有任何节点或工作负载
- **THEN** 系统 SHALL 返回各项计数为零的总览结果，而不是错误

#### Scenario: 统计不落库

- **WHEN** 多次请求同一集群的总览
- **THEN** 系统 SHALL NOT 在平台数据库中新增或更新任何集群资源状态记录

#### Scenario: 总览读取不产生操作审计

- **WHEN** 用户打开集群总览
- **THEN** 系统 SHALL NOT 生成操作审计记录，仅按既有规则在访问日志中保留请求元数据

### Requirement: 统计口径按授权范围收敛

总览 MUST 以当前用户身份通过 Impersonation 读取集群数据，只统计该用户有权读取的资源。用户无权限的命名空间 MUST NOT 计入任何数量，也 MUST NOT 通过总数、错误信息或字段存在性暴露其资源分布。

当跨命名空间读取被 apiserver 拒绝时，系统 SHALL 退回到逐个读取用户可见命名空间的方式继续汇总，而不是直接失败。

#### Scenario: 普通用户只能看到部分命名空间

- **WHEN** 用户仅对 `default` 命名空间有 Pod 读权限，集群其他命名空间存在大量 Pod
- **THEN** 总览中的 Pod 与工作负载计数 SHALL 只包含 `default` 命名空间的对象

#### Scenario: 跨命名空间读取被拒绝

- **WHEN** 用户没有集群级列表权限，跨命名空间读取返回拒绝
- **THEN** 系统 SHALL 改为按用户可见命名空间逐个读取并汇总，且结果中标注统计范围

### Requirement: 部分数据不可用时的降级

任一统计项读取失败（无权限、超时、聚合 API 不可用）MUST NOT 导致整个总览请求失败。系统 SHALL 返回其余可读取的数据，并在响应中显式列出不可用的统计项。

#### Scenario: 事件读取失败

- **WHEN** 最近事件读取因权限或错误失败，而节点与 Pod 数据可正常读取
- **THEN** 响应 SHALL 返回节点与 Pod 统计，并在不可用项中标注事件，页面 SHALL 对事件区域展示降级提示而不是空白或全局错误

#### Scenario: 集群完全不可达

- **WHEN** 目标集群健康状态为不可达
- **THEN** 总览接口 SHALL 返回明确的不可达错误，页面 SHALL 展示降级提示与重试入口

### Requirement: 异常判定口径

节点就绪 MUST 依据节点 Ready condition 判定；容量 MUST 使用节点可分配（allocatable）值汇总。异常工作负载 MUST 至少覆盖：非 Running/Succeeded 的 Pod、容器处于 CrashLoopBackOff 或 ImagePullBackOff 的 Pod，以及就绪副本少于期望副本的 Deployment/StatefulSet 和就绪数少于期望数的 DaemonSet。

#### Scenario: 副本不足的工作负载

- **WHEN** 一个 Deployment 期望 3 个副本但只有 2 个就绪
- **THEN** 该 Deployment SHALL 计入异常工作负载，总览中异常数 SHALL 加一

#### Scenario: 容器反复崩溃

- **WHEN** 一个 Pod 处于 Running 状态但其容器处于 CrashLoopBackOff
- **THEN** 该 Pod SHALL 计入异常，且不重复计入普通 Running 数量以外的分类

### Requirement: 总览页面

控制台 SHALL 提供「集群总览」页面，并作为用户进入某个集群后的默认落地页。页面 MUST 展示节点健康、可分配容量、Pod 状态分布、异常工作负载与最近事件，支持手动刷新并显示数据生成时间，且 MUST 区分加载中、空数据、加载失败与降级四种状态。

总览页面的任何计数 MUST NOT 被用作授权依据；页面中的对象入口在用户点击后仍由服务端重新校验权限。

#### Scenario: 进入集群默认看到总览

- **WHEN** 用户选择或进入一个集群
- **THEN** 界面 SHALL 打开该集群的总览页，而不是直接进入某个资源列表

#### Scenario: 无工作负载的空状态

- **WHEN** 用户可见范围内没有任何工作负载
- **THEN** 页面 SHALL 展示空状态说明，并保留节点与容量等可用信息

#### Scenario: 总览中的入口越权

- **WHEN** 用户从总览页点击一个其无权查看详情的对象
- **THEN** 服务端 SHALL 按既有授权规则拒绝，界面 SHALL 展示无权限状态
