# metadata-store Specification

## Purpose
规定 KubeDeck 平台元数据存储的引擎选择、仓储行为契约、迁移与启动语义，以及审计持久化在受支持引擎上的共同保证，使部署方可以在 PostgreSQL 与 MySQL 之间选择而不改变平台可见行为。

## Requirements

### Requirement: 可配置的元数据存储引擎

系统 SHALL 支持 PostgreSQL 15+ 与 MySQL 8.0.20+ 作为平台元数据存储，并通过连接串选择引擎；可选地显式指定驱动。未显式指定驱动时，系统 MUST 依据连接串的协议前缀识别引擎；无法识别且无法安全回退时 MUST 阻止启动并指出连接配置字段。显式驱动与连接串冲突、或驱动不受支持时，启动 MUST 失败并给出可定位的错误。

#### Scenario: 现有 PostgreSQL 部署无需改动

- **WHEN** 已有部署只提供 PostgreSQL 连接串，未设置驱动配置
- **THEN** 系统 SHALL 按 PostgreSQL 启动，迁移与审计行为与升级前一致

#### Scenario: 使用 MySQL 启动

- **WHEN** 连接串指向 MySQL 8.0.20+，驱动为 mysql 或可由连接串识别
- **THEN** 系统 SHALL 连接 MySQL、应用该引擎的迁移并进入就绪状态

#### Scenario: 驱动与连接串冲突

- **WHEN** 显式驱动为 mysql，但连接串是 PostgreSQL 协议
- **THEN** 启动 MUST 失败，并指出数据库驱动与连接串不一致

#### Scenario: 不支持的驱动

- **WHEN** 驱动配置为不受支持的值
- **THEN** 启动 MUST 失败，并列出受支持的取值

### Requirement: 跨引擎仓储行为一致

在两种受支持引擎上，用户、集群、凭据、角色绑定、平台设置与审计的仓储操作 SHALL 具有相同的可见语义，包括创建、更新、幂等 upsert、分页与排序、唯一冲突映射、记录缺失映射。API 与服务层 MUST NOT 因引擎不同而改变分支或响应；引擎差异 MUST 限定在存储实现内部。

#### Scenario: 唯一冲突映射一致

- **WHEN** 在任一引擎上创建名称已存在的集群
- **THEN** 系统 SHALL 返回冲突错误，且不得留下部分写入的凭据

#### Scenario: 缺失记录映射一致

- **WHEN** 在任一引擎上按不存在的标识删除角色绑定
- **THEN** 系统 SHALL 返回记录不存在错误

#### Scenario: 列表分页一致

- **WHEN** 在任一引擎上使用相同分页参数列出用户或审计记录
- **THEN** 返回顺序与每页数量 SHALL 一致

### Requirement: 审计持久化不变式

审计记录在两种引擎上 MUST 只追加：对既有审计记录的更新或删除 MUST 被存储层拒绝。每条记录 MUST 包含与前一条记录衔接的哈希链；校验 MUST 能定位被篡改的记录。审计记录 MUST 按月分区，保留期清理 MUST 只删除整月分区，MUST NOT 删除当前月份或兜底分区。升级后，已有引擎内的审计链 MUST 仍可校验。

#### Scenario: 只追加

- **WHEN** 任一引擎上尝试更新或删除审计记录
- **THEN** 存储层 MUST 拒绝该操作

#### Scenario: 篡改检测

- **WHEN** 绕过只追加保护修改某条审计记录后执行链校验
- **THEN** 校验 SHALL 返回该记录为断裂点

#### Scenario: 保留期清理

- **WHEN** 某整月分区完全早于保留期截止时间
- **THEN** 清理 SHALL 删除该分区，且 SHALL 保留当前月与兜底分区

### Requirement: 迁移与并发启动

每个受支持引擎的迁移 MUST 随二进制发布并在启动时按版本顺序应用；已应用迁移的内容发生变化时 MUST 阻止启动。多个实例并发启动时，迁移 MUST 串行执行，MUST NOT 产生重复建表或重复记录。某次迁移失败时启动 MUST 失败；修复或重试后 MUST 能从已成功应用的部分继续。

#### Scenario: 并发启动串行迁移

- **WHEN** 两个实例同时对新库启动
- **THEN** 迁移 SHALL 只执行一次，两个实例最终看到相同的 schema 版本

#### Scenario: 迁移内容漂移

- **WHEN** 已应用迁移文件的校验和与记录不一致
- **THEN** 启动 MUST 失败并指出迁移版本

#### Scenario: 迁移失败后重试

- **WHEN** 迁移在部分语句成功后失败，修复原因后再次启动
- **THEN** 系统 SHALL 在已完成部分之上继续，MUST NOT 要求手工清空数据库

### Requirement: 引擎切换边界

平台 MUST NOT 在切换存储引擎时自动搬迁既有元数据；切换引擎 MUST 视为新库初始化。审计哈希链 MUST 只在单一引擎内连续；跨引擎复制审计记录后 MUST NOT 要求链校验通过。部署文档 MUST 说明切换引擎需要重新登记集群与权限。

#### Scenario: 空库初始化

- **WHEN** 使用一个空 MySQL 库启动平台
- **THEN** 系统 SHALL 创建全部平台表、预置角色与默认设置，并允许重新登记集群

#### Scenario: 跨引擎审计链

- **WHEN** 将 PostgreSQL 的审计记录复制到 MySQL 后校验链
- **THEN** 校验 SHALL 不承诺跨引擎连续，且不得声称原链已完整迁移

### Requirement: 双引擎数据层验证

数据层集成用例 MUST 使用同一套行为用例分别连接真实 PostgreSQL 与真实 MySQL 执行；本地缺少某一引擎环境时 MAY 跳过该引擎，但 CI MUST 提供两种真实数据库并实际执行，MUST NOT 因缺少数据库环境而跳过。

#### Scenario: 本地单引擎运行

- **WHEN** 本地只配置 PostgreSQL 测试连接
- **THEN** PostgreSQL 用例 SHALL 实际执行，MySQL 用例 MAY 跳过并说明原因

#### Scenario: CI 双引擎执行

- **WHEN** CI 运行后端测试
- **THEN** 同一套数据层用例 SHALL 分别连接 PostgreSQL 与 MySQL 实际执行，且任一引擎失败 MUST 使 CI 失败
