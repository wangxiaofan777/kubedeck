# 三项关键技术验证报告

> 日期：2026-11-12
> 环境：Windows 10 (win32 10.0.26300) + WSL2 Ubuntu 26.04 + k3s v1.37.2
> 结论：**三项全部通过**，Impersonation 方案可行，exec 链路可用。

---

## 1. 测试环境

| 组件 | 版本 / 位置 | 说明 |
|---|---|---|
| Go (Windows) | 1.27.4，`C:\Users\maosong.wang\tools\go` | 交叉验证用 |
| Go (WSL) | 1.27.4，`/usr/local/go` | 实际编译运行位置 |
| k3s | v1.37.2+k3s1（K8S 1.37） | WSL2 内，systemd 管理 |
| kubectl | v1.36.1（Windows） | 通过 `127.0.0.1:6443` 访问 |
| kubeconfig | `.testenv/k3s.yaml` | 从 `/etc/rancher/k3s/k3s.yaml` 导出 |
| 镜像加速 | `docker.m.daocloud.io` / `docker.1ms.run` | 直连 Docker Hub 的 CloudFront 被墙 |

### 1.1 环境搭建命令（可复现）

```bash
# WSL 内安装 k3s（使用国内镜像源）
curl -fsSL https://rancher-mirror.rancher.cn/k3s/k3s-install.sh -o /tmp/k3s-install.sh
INSTALL_K3S_MIRROR=cn sh /tmp/k3s-install.sh \
  --write-kubeconfig-mode=644 --disable=traefik --tls-san=127.0.0.1

# containerd 镜像加速
cat > /etc/rancher/k3s/registries.yaml <<'EOF'
mirrors:
  docker.io:
    endpoint:
      - "https://docker.m.daocloud.io"
      - "https://docker.1ms.run"
  registry.k8s.io:
    endpoint:
      - "https://docker.m.daocloud.io"
EOF
systemctl restart k3s
```

```bash
# WSL 内安装 Go
curl -fL https://mirrors.aliyun.com/golang/go1.27.4.linux-amd64.tar.gz -o /tmp/go.tgz
rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz
```

---

## 2. 验证项与结果

### 验证一：dynamic client + discovery 列 Pod

```bash
./verify -mode=list -kubeconfig /etc/rancher/k3s/k3s.yaml -ns kube-system
```

结果：

```
[list] WARN partial discovery failure (tolerated): unable to retrieve the complete list of server APIs:
       metrics.k8s.io/v1beta1: stale GroupVersion discovery: metrics.k8s.io/v1beta1
[list] discovery ok, preferred resource groups: 27, pods discovered: true
[list] namespace=kube-system pod count=4
  - coredns-577d995dff-6rxhm                      phase=Running
  - helm-install-gateway-api-crd-j7qvm            phase=Succeeded
  - local-path-provisioner-56c6584955-858x7       phase=Running
  - metrics-server-778c4649b4-ml6q9               phase=Running
```

**通过的判定**：不依赖 typed client，成功发现 27 个 API 组并用 `dynamic.Interface` 列出 Pod。

**发现的真实问题（重要）**：`discovery.ServerPreferredResources()` 在任何一个聚合 API 不可用时**整体返回错误**。本次是 metrics-server 刚启动、聚合层尚未就绪导致的 `stale GroupVersion discovery`。真实产品里这种情况很常见（metrics-server 重启、webhook 故障）。

**对设计的修正**：资源发现必须容忍部分失败：

```go
rl, err := dc.ServerPreferredResources()
if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
    return err // 只有非"部分失败"才中止
}
// 用已发现的资源继续，并在 UI 上标注 xxx API 暂不可用
```

这条已写入 Phase 0 的强制实现要求。

---

### 验证二：Impersonation + K8S 原生 RBAC

```bash
./verify -mode=impersonate -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default -user alice
```

结果：

```
[impersonate] step1 user=alice ns=default before-grant:
  err=pods is forbidden: User "alice" cannot list resource "pods" in API group "" in the namespace "default"
[impersonate] step2 after-grant: err=<nil> pods=0
[impersonate] step3 cross-namespace kube-system:
  err=pods is forbidden: User "alice" cannot list resource "pods" in API group "" in the namespace "kube-system"
[impersonate] SSAR allowed=true
  reason="RBAC: allowed by RoleBinding \"kubedeck-view-pods/default\" of Role \"kubedeck-view-pods\" to User \"alice\""
```

四个断言全部符合预期：

| # | 断言 | 结果 |
|---|---|---|
| 1 | 未授权时以 alice 访问 → 403 | ✅ |
| 2 | 创建 RoleBinding 后访问 → 成功 | ✅ |
| 3 | 跨 namespace 访问 → 403（边界生效） | ✅ |
| 4 | `SelfSubjectAccessReview` 返回 allowed + reason | ✅ |

**结论**：
- 平台不需要 `cluster-admin`，用 admin 凭据发起带 `Impersonate-User` 的请求即可；
- 权限判定完全由 apiserver 完成，与设计文档 §4.2 一致；
- `SelfSubjectAccessReview` 的 `reason` 字段可以直接展示给用户（"由 xxx RoleBinding 授予"），对排障很友好，建议前端详情页展示。

**注意**：本次用的是 k3s 的 `system:masters` 凭据（天然拥有 impersonate 权限）。生产接入时平台 SA 的 ClusterRole 必须显式授予：

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubedeck-impersonator
rules:
  - apiGroups: [""]
    resources: ["users", "groups", "serviceaccounts"]
    verbs: ["impersonate"]
  - apiGroups: ["authentication.k8s.io"]
    resources: ["userextras/*"]
    verbs: ["impersonate"]
```

---

### 验证三：exec 进入容器

```bash
./verify -mode=prepare ...   # 创建 busybox 测试 Pod
./verify -mode=exec -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default -pod kubedeck-exec-test
```

结果：

```
[exec] target default/kubedeck-exec-test
exec-ok
kubedeck-exec-test
uid=0(root) gid=0(root) groups=0(root),10(wheel)
[exec] success
```

**结论**：
- `remotecommand.NewSPDYExecutor` 在 client-go v0.37.2 + K8S 1.37 上仍可用；
- 浏览器侧只需把 WebSocket 帧转成 `StreamOptions{Stdin/Stdout/Stderr/TTY/ResizeQueue}`，链路已验证；
- 后续可评估 `NewWebSocketExecutor`（K8S 1.31+ 推荐，SPDY 已标记废弃）——建议 Phase 2 实现时直接上 WebSocket executor，减少一层协议。

---

## 3. 环境相关的坑（交付时要注意）

| 问题 | 现象 | 处理 |
|---|---|---|
| Docker Hub 被墙 | `production.cloudfront.docker.com` DNS 失败，Pod 卡 ContainerCreating | 配 `registries.yaml` 镜像加速 |
| WSL 内 flannel 启动竞态 | `failed to load flannel 'subnet.env'`，Pod 一直 ContainerCreating | 重启后删掉卡住的 Pod 让它重建；或等 `/run/flannel/subnet.env` 生成后重启 k3s |
| Windows→WSL localhost 转发不稳定 | kubectl 间歇性 `connection refused`，过一会儿又恢复 | 验证/调试统一在 WSL 内跑；CI 不要依赖该转发 |
| 无法直连 WSL IP | `Test-NetConnection 172.20.x.x:6443` = False（防火墙） | 同上，用 WSL 内部 localhost |
| 模块下载慢 | `proxy.golang.org` 不可用 | `GOPROXY=https://goproxy.cn,direct` |

---

## 4. 对设计文档的影响

| 项 | 变化 |
|---|---|
| D-02 Impersonation | **确认可行**，追加"平台 SA 最小 ClusterRole"清单 |
| D-03 exec | **确认可行**，Phase 2 直接用 WebSocket executor |
| 资源发现 | 新增强制要求：容忍 `IsGroupDiscoveryFailedError`，UI 标注降级 |
| 测试环境 | 确认 WSL2 + k3s 方案可复现，作为 `make dev-cluster` 的蓝本 |

---

## 5. 复现方式

```bash
# 1. 确保 WSL 内 k3s 运行
wsl.exe -d Ubuntu -u root -- systemctl is-active k3s

# 2. 编译（WSL 内）
wsl.exe -d Ubuntu -u root -- bash -lc \
  "cd /root/verify && GOPROXY=https://goproxy.cn,direct /usr/local/go/bin/go build -o verify ."

# 3. 依次运行
wsl.exe -d Ubuntu -u root -- bash -lc "cd /root/verify && ./verify -mode=list -kubeconfig /etc/rancher/k3s/k3s.yaml -ns kube-system"
wsl.exe -d Ubuntu -u root -- bash -lc "cd /root/verify && ./verify -mode=impersonate -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default -user alice"
wsl.exe -d Ubuntu -u root -- bash -lc "cd /root/verify && ./verify -mode=prepare -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default && ./verify -mode=exec -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default -pod kubedeck-exec-test"
```

---

## 6. 多版本兼容矩阵（2026-11-12）

按 design.md D12 的支持窗口（1.34–1.37），三个验证程序在窗口两端各跑一遍。换装步骤与原始输出见 [docs/local-environment.md §7.1](../docs/local-environment.md)。

| K8S | client-go | list | impersonate+SSAR | exec |
|---|---|---|---|---|
| 1.37.2 (k3s v1.37.2+k3s1) | v0.37.2 | ✅ | ✅ | ✅ |
| 1.34.13 (k3s v1.34.13+k3s1) | v0.37.2 | ✅ | ✅ | ✅ |

结论：**窗口两端全绿**。`ServerPreferredResources` 部分失败容忍、Impersonation + SSAR、SPDY exec 在 1.34 上行为与 1.37 一致（1.34 上聚合 API 未就绪时同样触发 partial discovery 分支，容忍逻辑跨版本有效）。低于 1.34 的接入拒绝行为不在本矩阵内，由平台侧单元测试覆盖（tasks 4.8）。

> 源码：`verify/main.go`（单文件，约 250 行，四个子命令：list / prepare / impersonate / exec）