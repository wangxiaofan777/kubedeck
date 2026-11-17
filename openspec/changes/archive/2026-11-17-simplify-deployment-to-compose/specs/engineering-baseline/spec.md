## MODIFIED Requirements

### Requirement: 会话与部署副本一致性

平台 SHALL 以保证会话可用的方式部署。当前登录会话与 OIDC 访问令牌保存在进程内，部署配置 MUST 只运行一个 KubeDeck 实例，MUST NOT 提供副本数、伸缩或横向扩容开关；部署文档 MUST 说明多实例会导致随机认证失败，并在引入共享会话存储前不支持多副本。

#### Scenario: 默认单副本

- **WHEN** 使用仓库提供的 Docker Compose 配置启动
- **THEN** SHALL 只启动一个 KubeDeck 实例，且文档说明会话保存在进程内

#### Scenario: 显式配置多副本

- **WHEN** 部署方在同一数据库上启动第二个 KubeDeck 实例或尝试扩容
- **THEN** 部署文档 MUST 说明该配置不受支持，并指出需要先引入共享会话存储

## ADDED Requirements

### Requirement: 部署制品收敛

仓库 SHALL 只维护 Dockerfile 与单文件 Docker Compose 部署（KubeDeck 应用 + PostgreSQL），并只保留一份部署文档。仓库 MUST NOT 维护 Helm Chart、离线打包脚本，或同一部署方式的数据库/离线覆盖文件与重复环境变量示例。TLS 终止、外部数据库与镜像分发 SHALL 由部署环境负责，并在部署文档中说明。

可观测性只保留应用自身的 `/metrics` 端点与结构化日志；仓库 MUST NOT 随版本分发与特定监控栈绑定的告警规则。

#### Scenario: 部署入口唯一

- **WHEN** 按部署文档执行
- **THEN** 只需 `deploy/compose/docker-compose.yml`、`.env.example` 与一份部署文档，即可在现有 PostgreSQL、OIDC IdP 和 HTTPS 入口上启动完整控制台

#### Scenario: 不维护并行编排

- **WHEN** 检查 `deploy/` 与打包脚本
- **THEN** MUST NOT 存在 Helm Chart、Prometheus 告警规则、离线打包脚本或 Compose 多版本变体

#### Scenario: 内网交付

- **WHEN** 目标机器无法访问镜像仓库
- **THEN** 部署文档 SHALL 给出 `docker build`、`docker save`、`docker load` 的最小步骤，MUST NOT 要求额外的打包工具