## 1. OIDC 声明校验

- [x] 1.1 `OIDCClaims.EmailVerified` 改为可区分缺失与 false 的类型
- [x] 1.2 `OIDCConfig`/`Provider` 增加 `AllowUnverifiedEmail`，登录时按默认严格规则校验
- [x] 1.3 组声明规范化：去空白、去重、丢弃空值、限制数量与长度
- [x] 1.4 补充邮箱未验证拒绝、显式放宽、组规范化的单元测试
- [x] 1.5 配置层增加 `KUBEDECK_OIDC_ALLOW_UNVERIFIED_EMAIL` 并接入 `app.go`

## 2. 部署约束

- [x] 2.1 Helm `deployment.yaml` 在 `replicaCount > 1` 时渲染失败并说明原因
- [x] 2.2 `values.yaml` 注释与 `docs/deployment.md`、`deploy/compose/README.md` 写明单副本约束
- [x] 2.3 文档补充 IdP 声明要求与关闭邮箱校验的风险

## 3. 验证

- [x] 3.1 `go test ./cmd/... ./internal/...`
- [x] 3.2 `openspec validate harden-oidc-claims-and-deployment --strict`
- [x] 3.3 使用 `helm template` 验证默认单副本成功、显式多副本失败