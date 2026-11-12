# 端到端验收记录

环境：WSL2 Ubuntu + k3s v1.37.2，KubeDeck 通过 Helm 安装在 `kubedeck-system`，PostgreSQL 16（集群内测试实例），开发模式挂载 kubeconfig（仅本地验收）。

## 1. 安装

```bash
helm upgrade --install kubedeck deploy/helm/kubedeck -n kubedeck-system --wait
kubectl -n kubedeck-system get pods
```

结果：`kubedeck` 1/1 Running；`/healthz` 200；`/readyz` 返回 `ok`。

## 2. 登录

开发模式下首次 API 请求自动建立本地会话，页面显示“开发模式 / Local Developer”，平台管理员范围。生产 OIDC 登录需在真实 IdP 上按 [deployment.md](./deployment.md) 验证。

## 3. 接入集群

通过管理 API 提交平台 SA kubeconfig：

```text
create-cluster 201 local-reg v1.37.2+k3s1
```

结果：登记成功，读取到集群版本；列表 `/api/v1/clusters` 返回 `local` 与 `local-reg`。低于 1.34 与不可达地址由登记校验拒绝。

## 4. 浏览

访问 `/clusters/local-reg/pods?namespace=kubedeck-e2e`：

- 顶栏显示 `local-reg`、`v1.37.2+k3s1`。
- Pod 列表 5 行，包含 `crashy`、`follower`、`web-*` 等。
- 打开 Pod 详情，概览包含状态、容器、事件、Owner 引用；日志标签页返回 nginx 启动日志。
- 资源表格虚拟滚动：500 个 ConfigMap 单页仅渲染 12–13 行，滚动到底部能看到末尾对象；滚动过程中 DOM 行数保持稳定。

## 5. 伸缩

Deployment `kubedeck-e2e/web`：

1. 初始 2/2。
2. 通过界面“伸缩”改为 1，提交后显示 1/1，操作进度“操作已收敛”，耗时 0.0s。
3. 恢复为 2，显示 2/2，进度“操作已收敛”，耗时 2.6s。

## 6. 日志

Pod `web-6d64cbd67c-6j78k` 日志标签页返回 nginx 启动日志；跟随开关、暂停/恢复、关键字过滤可用。

另用持续输出的测试 Pod 验证暂停：

- 暂停前 26 行，暂停 3.5 秒后仍为 26 行；
- 恢复后持续增长到 38 行，暂停期间数据未丢失。

## 7. 审计

审计页（`/clusters/local/audit`）显示两条 `scale` 记录：

```text
2026/11/12 21:23:12  dev  local  scale  deployments / kubedeck-e2e / web  success 200  5ms
2026/11/12 21:22:55  dev  local  scale  deployments / kubedeck-e2e / web  success 200  12ms
```

每条带 requestId；导出 CSV/JSON 与查询条件一致。

## 8. 最小权限验证

平台 SA 只绑定 impersonate ClusterRole：

```text
sa can list pods: no
sa can impersonate users: yes
sa can create pods: no
impersonated list: 200 items: 1
can get/delete: 200 [True, False]
impersonated delete: 403 FORBIDDEN
```

结论：SA 本身无资源权限；以只读用户 alice 身份代请求可读，写操作被 apiserver 拒绝。

## 9. 安全自查

CSRF、SSRF、CSP、凭据脱敏、只读越权均通过，详见 [security-review.md](./security-review.md)。

## 10. 未覆盖项

- 生产 OIDC 登录（需要企业 IdP；本地未部署 IdP）。
- 多副本会话亲和（当前会话在进程内存，建议单副本）。
- 1000 节点 / 50k Pod 真实规模压测（见 [load-test.md](./load-test.md) 的模拟结果与限制）。
- 1.34 版本矩阵（见 [compatibility.md](./compatibility.md)）。