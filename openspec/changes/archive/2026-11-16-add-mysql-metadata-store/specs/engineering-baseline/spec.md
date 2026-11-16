## MODIFIED Requirements

### Requirement: 统一质量闸门

仓库 SHALL 提供与 CI 一致的本地检查入口，覆盖后端格式化、静态检查与单元测试，以及前端 lint、类型检查与构建；任一检查失败时命令 MUST 以非零状态退出。CI SHALL 同时连接真实 PostgreSQL 与真实 MySQL 执行数据层集成测试。

#### Scenario: 本地检查失败即阻断

- **WHEN** 代码存在格式、静态检查或类型错误
- **THEN** 统一检查命令 SHALL 返回非零状态并指出失败项

#### Scenario: CI 数据层测试真实执行

- **WHEN** CI 运行后端测试
- **THEN** 数据层集成用例 SHALL 分别连接 PostgreSQL 与 MySQL 实际执行，MUST NOT 因缺少任一数据库环境而跳过