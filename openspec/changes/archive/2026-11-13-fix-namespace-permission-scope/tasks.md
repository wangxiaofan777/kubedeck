## 1. 后端可见性判定

- [x] 1.1 在 `internal/api/access.go` 新增 `clusterVisible(ctx, id, cluster)`：管理员放行，非管理员存在任一该集群绑定即放行；补充单元测试覆盖管理员、有绑定、无绑定三种情况；执行 `go test ./internal/api -run TestClusterVisible` 通过
- [x] 1.2 重写 `internal/api/clusters.go` 的 `listNamespaces`：先取集群与 `clusterVisible`，管理员与 `*` 绑定走 impersonated `Namespaces()`，具体绑定直接返回去重排序后的命名空间，无绑定返回 403；不再先调用 `transport()`；新增测试覆盖四种身份与"绑定不泄露范围外名称"
- [x] 1.3 保留集群不可达时的 503 语义：需要访问 apiserver 的分支复用现有错误映射，补充测试断言不可达集群返回 `CLUSTER_UNREACHABLE`

## 2. 集群级接口准入

- [x] 2.1 将 `internal/api/resources.go` 的 `apiResources` 从 `accessibleCluster`（要求 `*`）改为 `clusterVisible`，使命名空间级用户也能拿到资源发现；新增测试断言其返回 200
- [x] 2.2 将 `internal/api/cluster_admin.go` 的 `getCluster`、`getClusterHealth` 改为 `clusterVisible`，并保持凭据字段仅对 `PlatformAdmin` 返回；新增测试断言命名空间级用户 200 且响应不含 `credential`
- [x] 2.3 复核 `listEvents`、跨命名空间资源列表与 WebSocket 准入未被放宽（仍要求 `*` 或具体 namespace）；执行 `go test ./internal/api` 全部通过

## 3. 前端上下文

- [x] 3.1 在 `web/src/app/ConsoleShell.tsx` 基于 `me.bindings` 计算 `clusterWide`（管理员或该集群 `*` 绑定），并据此决定命名空间选项是否包含"全部命名空间"；执行 `npm --prefix web run typecheck` 通过
- [x] 3.2 增加首次进入自动选择：`type.namespaced && !namespace && !clusterWide` 且已拿到命名空间列表时，用 `replace` 导航到首个可访问命名空间；避免重复触发与覆盖已有 namespace；执行 typecheck 并手工验证无循环跳转
- [x] 3.3 过滤导航菜单：`!clusterWide` 时隐藏 `namespaced === false` 的资源类型（Namespaces、Nodes）；执行 `npm --prefix web run lint` 与 typecheck 通过
- [x] 3.4 命名空间列表加载失败时给出明确提示（403 显示"无权限"，其他错误可重试），不把空列表静默当作"全部命名空间"；执行 typecheck 通过

## 4. 文档与验收

- [x] 4.1 更新 `docs/permission-matrix.md`：说明命名空间列表的来源、`*` 绑定需要目标集群 `list namespaces` 权限、开发模式固定为管理员无法验证隔离；人工核对与实现一致
- [x] 4.2 执行 `make check`（后端 lint/测试 + 前端 lint/typecheck）全部通过
- [x] 4.3 使用 OIDC 非管理员账号（绑定 `default` 与 `dev`）做端到端验收：命名空间下拉只有这两个、首次进入自动选中 `default`、无"全部命名空间"、Namespaces/Nodes 菜单不出现、直接访问未绑定命名空间显示 403；将结果记录到 `tasks.md` 备注
## 备注

- 4.2：Windows 环境没有 `make`，已逐项执行等价命令：`gofmt -l cmd internal`、`go vet ./cmd/... ./internal/...`、`staticcheck ./cmd/... ./internal/...`、`go test ./cmd/... ./internal/...`、`npm --prefix web run lint`、`npm --prefix web run typecheck`，全部通过。
- 4.3：用 `add-dev-nonadmin-mode` 提供的开发模式非管理员开关完成验收（等价于 OIDC 非管理员身份，平台准入仍按角色绑定执行）：`KUBEDECK_DEV_USER=alice KUBEDECK_DEV_PLATFORM_ADMIN=false`，alice 仅绑定 `default` 与 `dev`。浏览器实测结果：命名空间下拉只有 `default`、`dev` 两项；首次进入 Pods 自动跳转 `?namespace=default`；下拉中无"全部命名空间"；导航菜单不出现 Namespaces/Nodes；直接访问 `?namespace=kube-system` 显示"无访问权限 / 当前账号无权访问该命名空间或资源类型。"且不展示任何资源数量。
- 额外修复：`internal/audit/writer_test.go` 的重试测试用固定 30ms 等待断言降级状态，在 Windows 上偶发失败；改为按 deadline 轮询，避免与本次变更无关的 flake。
