## Context

当前实现已经能完成只读控制台的核心链路，但结构是逐次叠加的：`cmd/server/main.go` 同时负责配置、数据库、审计维护协程、HTTP 服务与优雅退出；`internal/api/handlers.go` 把路由参数、分页参数、JSON/YAML 响应和集群访问混在一起；`web/src/App.tsx` 同时承担布局、查询、轮询、选中状态与命名空间切换。

构建侧还有两个硬约束：前端产物通过 `go:embed` 进入单二进制，因此 `go build` 依赖前端产物目录存在；CI 的 Go 任务早于前端构建，且没有 PostgreSQL，导致全新检出编译失败、数据层测试静默跳过。这些问题必须在继续叠加认证与写操作前解决。

## Goals / Non-Goals

**Goals:**

- 把启动编排、HTTP 接口、集群访问、仓储职责拆到可独立测试的边界。
- 让全新检出、清理后重建、CI 三条路径的构建结果一致。
- 建立本地与 CI 一致的格式化、静态检查、单测与前端检查入口。
- 让配置错误、依赖不可用、退出过程具备明确且可观测的行为。

**Non-Goals:**

- 不引入认证、集群登记写接口、写操作、实时推送。
- 不改变页面信息架构、现有 API 路径与响应字段。
- 不引入服务层框架、依赖注入容器或前端路由（本轮只做结构收敛）。
- 不追求 100% 测试覆盖率；只保证关键路径与新增校验有测试。

## Decisions

### D1. 启动编排独立为 `internal/app`，用 errgroup 管理生命周期

`cmd/server/main.go` 只保留配置加载、日志初始化、信号上下文与 `app.Run`。`app.Run` 负责：连接数据库 → 迁移 → 构建集群注册表 → 构建路由 → 启动 HTTP → 启动审计维护协程；退出时按「取消 ctx → 等待后台协程 → 关闭 HTTP → 关闭连接池」顺序执行。

**理由**：当前 `defer st.Close()` 与后台协程存在竞态，且 `main` 无法被测试。**备选**：继续在 `main` 里堆叠（改动小，但不可测）；引入 wire/fx（本项目规模不值得）。

### D2. 接口层按资源拆分，依赖用消费方接口

`internal/api` 拆为 `router.go`（路由与依赖）、`clusters.go`、`resources.go`、`params.go`、`respond.go`、`health.go`、`errors.go`。handler 只依赖本包声明的最小接口（集群查找、数据库 ping），不直接依赖具体注册表类型。

**理由**：handler 可注入 fake 做 `httptest` 测试，同时避免为 MVP 造一层没有业务逻辑的 service。**备选**：新增 `internal/service`（等认证与写操作落地后再评估）；让 handler 继续持有具体类型（测试成本高）。

### D3. 前端按 feature 组织，查询逻辑下沉为 hooks

`App.tsx` 只做布局组合；`app/` 放 providers 与布局；`features/clusters`、`features/resources` 放页面级 hooks 与组件；`lib/api.ts` 统一请求与错误归一化，`lib/format.ts` 放时间/状态格式化。本轮不引入 react-router，URL 状态留到控制台信息架构任务。

**理由**：先把数据获取与视图解耦，避免继续在单组件里叠加轮询与选中逻辑。**备选**：直接上路由与全局 store（会与后续权限上下文设计冲突）。

### D4. 用受版本控制的占位文件保证 `go:embed` 可构建

把 embed 包放到 `internal/webui`，Vite 产物直接输出到 `internal/webui/dist`；在该目录放置受版本控制的 `.gitkeep`。`make web` 构建完成后重新创建该文件，`make clean` 也只清理生成产物。`server`/`test` 目标依赖该占位，而不是要求先构建前端。

**理由**：`go:embed all:dist` 在目录为空或不存在时直接编译失败。产物归到 `internal/webui` 后，`web/` 只保留 npm 工程；Go 命令统一限定为 `./cmd/... ./internal/...`，避免 `web/node_modules` 里的 Go 文件被当作本模块包（实际遇到 `flatted` 自带 Go 代码的情况）。**备选**：把产物留在 `web/dist` 且 Go 命令仍用 `./...`（会被 node_modules 污染）；改成构建标签或运行时加载目录（破坏单二进制交付）。

### D5. 质量闸门：gofmt + go vet + staticcheck；ESLint + Prettier

后端统一入口 `make lint` 执行 gofmt 检查、`go vet`、`staticcheck`（未安装时给出安装提示并以失败退出）；`make check` 追加 `go test ./...` 与前端 lint/typecheck。前端使用 ESLint 9 flat config + typescript-eslint + react-hooks，Prettier 保持现有无分号/单引号风格。

**理由**：不引入 golangci-lint 的聚合配置，减少工具版本与配置语法漂移；staticcheck 覆盖大部分高风险问题。**备选**：golangci-lint（生态更全，但版本升级会带来配置迁移成本，可后续替换）。

### D6. 配置在监听前一次性校验，YAML 严格解析

`config.Load` 之后调用 `Config.Validate`：监听地址非空、日志级别枚举、CORS 不允许 `*` 与凭据同时出现、主密钥必须为 32 字节 base64、前后密钥版本不得相同。YAML 使用 `KnownFields(true)`，未知键直接失败。

**理由**：错误配置在运行期表现为“看似正常但行为异常”，排查成本高。**备选**：只告警不阻断（生产环境容易带病启动）。

### D7. 健康与就绪分离，安全头按前端实际渲染方式收敛

`/healthz` 只表示进程存活，始终 200；`/readyz` 在配置了数据库时执行带 2 秒超时的 ping，失败返回 503。CSP 增加 `style-src 'self' 'unsafe-inline'`、`img-src`/`font-src` 的 `data:` 白名单，保留 `frame-ancestors 'none'` 与 `base-uri 'self'`。

**理由**：antd 5 通过运行时 `<style>` 注入样式，仅 `default-src 'self'` 会导致生产页面无样式。**权衡**：`unsafe-inline` 只放宽样式，不放宽脚本；后续可改为 nonce。

### D8. 迁移记录增加校验和，存量记录自动回填

`schema_migrations` 增加 `checksum` 列。执行器启动时 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`；已应用但校验和为空的记录用当前文件内容回填；已应用且校验和不匹配时返回错误并停止启动。

**理由**：避免有人修改已执行迁移导致环境间 schema 漂移。**备选**：引入完整迁移框架（依赖重、收益有限）。

### D9. 审计保留期读取失败时停止清理

`RetentionDays` 区分“未配置”（回退 180）与“读取失败”（返回错误）；维护任务遇到读取失败只告警并跳过本轮删除。

**理由**：用默认值继续删除可能误删仍在保留期内的审计数据。**权衡**：配置库故障期间分区不会回收，需要运维关注告警。

## Risks / Trade-offs

- **静态检查引入噪声** → 先用 `staticcheck` 默认规则与 `go vet`，不启用风格类检查；前端 ESLint 只启用 recommended 与 hooks 规则。
- **CSP 放宽 `style-src`** → 仅对样式生效，脚本仍为 `'self'`；在设计中记录后续 nonce 方案。
- **CI PostgreSQL 服务增加耗时** → 使用 service container + 健康检查，测试库独立，失败时保留日志。
- **迁移校验和会阻止“热修”已执行迁移** → 这是预期行为；需要变更 schema 时新增迁移文件。
- **Makefile 仍以 POSIX shell 为假设** → 文档明确在 WSL/Linux 执行；Windows 原生环境不作为支持路径。

## Migration Plan

1. 先在本地执行 `make clean && make build`，确认占位文件与构建顺序生效。
2. 启动时自动添加 `schema_migrations.checksum` 并回填，无需手工 SQL。
3. 回滚方式：本次不改变业务表结构与 API 契约，回退代码即可；已回填的校验和列可保留，不影响旧版本启动。
4. 发布前在 CI 跑通 PostgreSQL 集成测试与前端检查，避免只在本地通过。