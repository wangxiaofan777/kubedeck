## 1. 构建基线

- [x] 1.1 新增受版本控制的 `internal/webui/dist/.gitkeep`，并让 `make web` 在构建后、`make clean` 在清理后都保留该占位；验证清空产物目录后 `go build ./...` 仍然成功
- [x] 1.2 调整 Makefile：`server`/`test` 依赖 dist 占位，`build` 顺序显式化，新增 `fmt`/`check`/`web-dev` 目标；验证 `make clean && make build` 从零成功
- [x] 1.3 更新 `docs/local-environment.md` §7.2，改用 `make build` 并说明新 clone 的构建顺序

## 2. 后端结构

- [x] 2.1 新增 `internal/app.Run`，把数据库初始化、注册表、路由、HTTP 服务与审计维护协程收拢进去，`cmd/server/main.go` 只保留配置/日志/信号；验证启动与终止日志顺序正确，退出时不出现释放连接池后仍访问数据库
- [x] 2.2 HTTP 服务补齐 `ReadTimeout`/`WriteTimeout`/`IdleTimeout`；验证配置值与计划一致，慢连接不会长期占用
- [x] 2.3 拆分 `internal/api`：`clusters.go`、`resources.go`、`params.go`、`respond.go`、`health.go`，handler 依赖消费方接口；`writeJSON` 先编码到 buffer，新增 `/readyz`；验证 `httptest` 覆盖列表、详情、404、就绪 503
- [x] 2.4 拆分 `internal/kube`：注册表独立并支持 `Remove`/`Names`，注册不再发起网络请求；`rest.Config` 设置 10 秒超时；命名空间列表跟随 `continue` 分页；验证注册表单测与命名空间分页逻辑
- [x] 2.5 拆分 `internal/store`：`roles.go` 与 `audit_append.go`/`audit_query.go`/`audit_chain.go`/`audit_partitions.go`；保留期读取失败时跳过删除；`SetClusterAccess` 输入去重；迁移记录增加 checksum 并回填存量记录；验证集成测试全部通过
- [x] 2.6 配置增加 `Validate` 与严格 YAML 解析，覆盖地址、日志级别、CORS 通配、主密钥格式与版本冲突；验证 `config` 包表驱动单测通过

## 3. 前端结构

- [x] 3.1 按 `app/`、`features/clusters`、`features/resources`、`components/`、`lib/` 重组 `web/src`，查询逻辑下沉为 hooks，`App.tsx` 只做布局组合；验证 `npm run build` 通过且页面功能不变
- [x] 3.2 统一 `lib/api.ts` 的请求与错误归一化：网络错误转为结构化错误、4xx 不重试、支持 `AbortSignal`；验证失败请求在界面上显示可读错误而不是 `undefined`
- [x] 3.3 修复上下文切换残留：切换集群/命名空间/资源类型时清空抽屉与失效命名空间，列表切换使用 `keepPreviousData`；验证浏览器中切换后不再请求旧的 YAML
- [x] 3.4 补基础可访问性：表格行支持键盘打开、搜索框有 `aria-label`、命名空间选择器关联标签；验证键盘可完成“选类型 → 开详情”

## 4. 工具链与 CI

- [x] 4.1 新增 `.editorconfig`，补齐 `.gitignore` 的日志、覆盖率、缓存与构建信息文件；验证 `git status` 不出现意外产物
- [x] 4.2 前端加入 ESLint 9 + typescript-eslint + Prettier，新增 `lint`/`format`/`format:check`/`typecheck` 脚本；验证 `npm run lint && npm run typecheck` 通过
- [x] 4.3 `make lint` 执行 gofmt 检查、`go vet`、`staticcheck`，`make check` 追加后端测试与前端 lint/typecheck；验证存在格式问题时命令返回非零
- [x] 4.4 重写 CI：后端 job 挂 PostgreSQL service 并执行 `go test -race`，前端 job 执行 lint/typecheck/build，镜像 job 依赖前两者；补 `permissions`/`concurrency`/超时；新增 Dependabot；验证工作流 YAML 结构与命令可在本地等价复现
- [x] 4.5 本地安装并执行 `staticcheck ./...`，修复或明确豁免全部告警；验证零告警退出

## 5. 文档

- [x] 5.1 重写根 `README.md`：当前状态、真实目录树、前置依赖、快速开始、常用命令、测试矩阵；验证按文档在干净目录可完成构建
- [x] 5.2 更新 `AGENTS.md` 的当前状态与工程约定入口，删除“尚未编码”等过期描述；验证描述与 `tasks.md` 进度一致
- [x] 5.3 更新 `docs/README.md` 的文档索引，加入新变更与工程命令入口；验证链接可达

## 6. 验证与交付

- [x] 6.1 执行 `make clean && make build`，再执行 `go test ./... -race` 与 `make test-db`；验证全部通过
- [x] 6.2 执行 `make lint`、`npm run lint`、`npm run typecheck`、`npm run build`；验证全部零错误
- [x] 6.3 在 WSL 重启服务，浏览器验证样式、资源列表、YAML 抽屉、集群/命名空间切换；验证 CSP 无资源拒绝、切换无残留
- [x] 6.4 执行 `openspec validate add-kubedeck-mvp --strict` 与 `openspec validate refactor-engineering-baseline --strict`；验证均通过
- [x] 6.5 以 `refactor:` 提交本次工程化改动，保持不推送；验证 `git status` 干净且提交只包含结构/工具链/文档变更