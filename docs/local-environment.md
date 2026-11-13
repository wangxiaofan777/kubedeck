# 本地开发与测试环境

> 本文把所有环境相关操作固化成可复现步骤。目标：一台干净的 Windows 机器，照着做能在 30 分钟内跑通 KubeDeck 的三项基础验证。
>
> 与生产部署的区别见文末「环境差异」一节。

## 1. 前置条件

| 项 | 要求 | 检查命令 |
|---|---|---|
| Windows | 10/11，已启用虚拟化 | `systeminfo` 查看 Hyper-V 要求 |
| WSL2 | 已安装，默认发行版可用 | `wsl.exe -l -v` |
| WSL 内 systemd | `/etc/wsl.conf` 含 `systemd=true` | `wsl.exe -d Ubuntu -u root -- ps -p 1 -o comm=` 应输出 `systemd` |
| 磁盘 | ≥ 5 GB 可用 | `df -h /` |
| 内存 | ≥ 4 GB 可分配给 WSL | `free -m` |

如果 `ps -p 1 -o comm=` 输出的是 `init` 而不是 `systemd`，在 WSL 内创建或修改 `/etc/wsl.conf`：

```ini
[boot]
systemd=true
```

然后执行 `wsl.exe --shutdown` 并重新进入。

## 2. 组件与版本

| 组件 | 版本 | 位置 |
|---|---|---|
| Go | 1.27.4 | Windows：`C:\Users\<你>\tools\go`；WSL：`/usr/local/go` |
| k3s | v1.37.2+k3s1（K8S 1.37） | WSL：`/usr/local/bin/k3s` |
| kubectl | v1.36.1（或 k3s 自带） | Windows / WSL |
| client-go | v0.37.2 | 由 `verify/go.mod` 锁定 |

> **版本原则**：平台支持窗口为 K8S 1.34–1.37（client-go v0.37.2 对应上游 n-3 偏移），硬地板 1.24。接入时读取 `/version`，低于 1.34 拒绝接入（返回 400）。依据见 [design.md D12](../openspec/changes/archive/2026-11-13-add-kubedeck-mvp/design.md)。版本比较使用 `/version` 返回的 `major`/`minor` 字段，不要解析 `gitVersion`（k3s 的形如 `v1.37.2+k3s1`，带构建后缀）。

## 3. 安装 k3s（WSL 内）

Docker Hub 的镜像分发走 CloudFront，国内网络下大概率拉取失败，因此安装时直接指定国内镜像源，并关闭用不到的 Traefik：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc '
  curl -fsSL --retry 3 https://rancher-mirror.rancher.cn/k3s/k3s-install.sh -o /tmp/k3s-install.sh
  INSTALL_K3S_MIRROR=cn sh /tmp/k3s-install.sh \
    --write-kubeconfig-mode=644 --disable=traefik --tls-san=127.0.0.1
'
```

安装脚本会完成：写入 `/usr/local/bin/k3s`、注册 `k3s.service`、生成 `/etc/rancher/k3s/k3s.yaml`、启用开机自启。

检查：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc 'systemctl is-active k3s && k3s kubectl get nodes'
```

节点状态应为 `Ready`，版本 `v1.37.2+k3s1`。

## 4. 配置 containerd 镜像加速

原因：直连 `registry-1.docker.io` 时元数据可以拿到，但 blob 会被重定向到 `production.cloudfront.docker.com`，该域名在部分网络下不可达，表现为 Pod 长时间 `ContainerCreating`，事件里是 `failed to pull image ... dial tcp: lookup production.cloudfront.docker.com`。

写入 `/etc/rancher/k3s/registries.yaml`：

```yaml
mirrors:
  docker.io:
    endpoint:
      - "https://docker.m.daocloud.io"
      - "https://docker.1ms.run"
  registry.k8s.io:
    endpoint:
      - "https://docker.m.daocloud.io"
```

然后重启 k3s：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc 'systemctl restart k3s && sleep 20 && systemctl is-active k3s'
```

验证加速是否生效（能看到分层下载进度即为成功）：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc 'k3s ctr -n k8s.io images pull docker.io/library/busybox:1.36'
```

## 5. 安装 Go

### 5.1 WSL 内（验证程序的实际编译环境）

```bash
wsl.exe -d Ubuntu -u root -- bash -lc '
  curl -fL --retry 2 https://mirrors.aliyun.com/golang/go1.27.4.linux-amd64.tar.gz -o /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
  /usr/local/go/bin/go version
'
```

### 5.2 Windows 侧（可选）

仅在需要本地 IDE 补全或 Windows 侧构建时安装。从阿里云镜像下载 zip 后解压到任意目录，把 `<解压目录>\go\bin` 加入 PATH：

```powershell
# 示例：解压到 C:\Users\<你>\tools\go
Expand-Archive -Path go1.27.4.windows-amd64.zip -DestinationPath C:\Users\<你>\tools
```

> 不要用 `winget install GoLang.Go`：在受限网络下会静默卡在源同步，且不报错。

### 5.3 模块代理

```bash
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
```

国内直连 `proxy.golang.org` 基本不可用，必须设置。

## 6. 导出 kubeconfig

```bash
wsl.exe -d Ubuntu -u root -- bash -lc \
  "mkdir -p /mnt/d/AI_Coding/kubedeck/.testenv && \
   install -m 0644 /etc/rancher/k3s/k3s.yaml /mnt/d/AI_Coding/kubedeck/.testenv/k3s.yaml"
```

该文件等价于集群管理员凭据：已在 `.gitignore` 中排除，不要提交、不要外发。

## 7. 运行三项验证

验证代码位于 `verify/`，单文件、四个子命令。先编译：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc '
  mkdir -p /root/kubedeck-verify
  cp /mnt/d/AI_Coding/kubedeck/verify/{main.go,go.mod,go.sum} /root/kubedeck-verify/
  cd /root/kubedeck-verify
  GOPROXY=https://goproxy.cn,direct /usr/local/go/bin/go build -o verify .
'
```

依次执行：

```bash
# 1) dynamic client + discovery（含部分发现失败容忍）
wsl.exe -d Ubuntu -u root -- bash -lc \
  "cd /root/kubedeck-verify && ./verify -mode=list -kubeconfig /etc/rancher/k3s/k3s.yaml -ns kube-system"

# 2) Impersonation + 原生 RBAC
wsl.exe -d Ubuntu -u root -- bash -lc \
  "cd /root/kubedeck-verify && ./verify -mode=impersonate -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default -user alice"

# 3) exec
wsl.exe -d Ubuntu -u root -- bash -lc \
  "cd /root/kubedeck-verify && ./verify -mode=prepare -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default"
wsl.exe -d Ubuntu -u root -- bash -lc \
  "cd /root/kubedeck-verify && ./verify -mode=exec -kubeconfig /etc/rancher/k3s/k3s.yaml -ns default -pod kubedeck-exec-test"
```

预期结果与判读方式见 [verify/README.md](../verify/README.md)。

> 重复执行时，第 2 项会因上一轮留下的 RoleBinding 而失去「未授权 → 授权」的对比。先清理：
> `kubectl delete rolebinding,role -n default kubedeck-view-pods`。
> 第 3 项若 Pod 处于 `Unknown/Terminated`（WSL 重启导致），先删除该 Pod 再执行 `-mode=prepare`。

### 7.1 多版本矩阵验证（换装 k3s）

兼容矩阵（design.md D12，窗口 1.34–1.37）靠在窗口两端实际跑 §7 的三项验证产出。换装用 k3s 独立数据目录实现，原集群状态不动、可随时切回：

```bash
# 1) 停止当前集群（数据保留在默认目录 /var/lib/rancher/k3s）
wsl.exe -d Ubuntu -u root -- bash -lc 'systemctl stop k3s'

# 2) 查询镜像上该 minor 的最新 tag（注意镜像 tag 用 -k3s1 而非 +k3s1）
wsl.exe -d Ubuntu -u root -- bash -lc 'curl -s https://rancher-mirror.rancher.cn/k3s/channels/v1.34'

# 3) 安装到独立数据目录（kubeconfig 仍写到 /etc/rancher/k3s/k3s.yaml，会被覆盖）
wsl.exe -d Ubuntu -u root -- bash -lc '
  INSTALL_K3S_MIRROR=cn INSTALL_K3S_VERSION=v1.34.13-k3s1 sh /root/k3s-install.sh \
  --data-dir /var/lib/rancher/k3s-v134 --disable=traefik --tls-san=127.0.0.1 --write-kubeconfig-mode=644'

# 4) 等节点 Ready 后，按 §7 原样跑三项验证；另读取版本留档：
wsl.exe -d Ubuntu -u root -- bash -lc 'k3s kubectl get --raw /version'

# 5) 切回原集群：停止、用原参数重装（不带 --data-dir 即恢复默认目录的存量状态）
wsl.exe -d Ubuntu -u root -- bash -lc '
  systemctl stop k3s &&
  INSTALL_K3S_MIRROR=cn INSTALL_K3S_VERSION=v1.37.2-k3s1 sh /root/k3s-install.sh \
  --disable=traefik --tls-san=127.0.0.1 --write-kubeconfig-mode=644'

# 6) 重导出 kubeconfig（§6）并清理临时数据目录
wsl.exe -d Ubuntu -u root -- bash -lc 'rm -rf /var/lib/rancher/k3s-v134'
```

> `/root/k3s-install.sh` 不存在时先重新下载：
> `curl -fsSL https://rancher-mirror.rancher.cn/k3s/k3s-install.sh -o /root/k3s-install.sh`。

**已实测的矩阵结果（2026-11-12）**：

| K8S 版本 | k3s | discovery+list | impersonate+SSAR | exec | 版本读取 |
|---|---|---|---|---|---|
| 1.37 | v1.37.2+k3s1 | ✅ 27 组，部分失败容忍触发 | ✅ 四断言全过 | ✅ | ✅ major=1 minor=37 |
| 1.34 | v1.34.13+k3s1 | ✅ 24 组，部分失败容忍触发 | ✅ 四断言全过 | ✅ | ✅ major=1 minor=34 |

两组版本均为全新独立数据目录环境；1.37 行为原始验证后经换装往返复测（集群状态与 Pod 名完全一致，证明换装可逆）。1.34 侧 RoleBinding/busybox 等临时产物随数据目录清理一并移除。低于 1.34 的拒绝行为（tasks 4.8）为纯逻辑，由单元测试覆盖，不在矩阵内。

### 7.2 本地运行 Web 控制台（开发模式）

开发模式用本地 kubeconfig 直接接入集群，跳过 OIDC 与数据库（生产路径在后续任务实现）。本机 k3s 在 WSL 内，Windows 侧直连 `127.0.0.1:6443` 不可达，因此**后端必须在 WSL 内运行**，并监听 `0.0.0.0` 供 Windows 浏览器访问：

```bash
# 1) 首次构建前端（Windows 侧；WSL 内没有原生 npm）
cd /d/AI_Coding/kubedeck/web && npm ci --no-audit --no-fund && npm run build

# 2) WSL 内构建并启动后端
wsl.exe -d Ubuntu -u root -- bash -lc '
  cd /mnt/d/AI_Coding/kubedeck &&
  make server &&
  KUBEDECK_ADDR=0.0.0.0:8080 \
  KUBEDECK_DEV_KUBECONFIG=/mnt/d/AI_Coding/kubedeck/.testenv/k3s.yaml \
  KUBEDECK_DEV_CLUSTER_NAME=local \
  ./bin/kubedeck
'
```

`make server` 只依赖 `internal/webui/dist` 中的占位文件，因此前端未构建时后端也能编译；界面要看到资源列表则需要先执行第 1 步的 `npm run build`。若 WSL 内已安装 Node，可直接用 `make build` 一条命令完成前后端构建。之后只改后端时重复 `make server` 即可。

Windows 浏览器打开 <http://localhost:8080>。前端联调时改用 Vite dev server（`/api` 与 WS 已代理到 8080）：

```bash
cd web && npm run dev   # http://localhost:5173
```

| 环境变量 | 作用 |
|---|---|
| `KUBEDECK_DEV_KUBECONFIG` | 开发模式集群的 kubeconfig 路径（必填） |
| `KUBEDECK_DEV_CLUSTER_NAME` | 集群展示名，默认 `local` |
| `KUBEDECK_DEV_USER` | 可选；以该用户身份 Impersonation（默认用 kubeconfig 自身身份） |
| `KUBEDECK_DEV_PLATFORM_ADMIN` | 可选；开发身份是否拥有平台管理员权限，默认 `true`，仅接受 `true`/`false` |
| `KUBEDECK_ADDR` | 监听地址，默认 `:8080` |

> dev 模式没有任何登录校验，只能用于本地开发，不要暴露到网络。设置 `KUBEDECK_DEV_PLATFORM_ADMIN=false` 后，平台准入改为按数据库中的角色绑定执行（通常配合 `KUBEDECK_DEV_USER` 使用），便于本地验收按权限收敛的功能；此时需要先启动数据库并写入对应用户的绑定。

需要平台元数据表（集群登记、用户、审计）时，启动前先按 §7.3 加载数据库连接串。

### 7.3 本地 PostgreSQL（平台元数据与审计）

平台元数据（用户、集群登记、凭据密文、角色绑定、审计）存 PostgreSQL；集群资源状态一律不入库。本机 WSL 内已装 PostgreSQL 18（生产要求 15+）。

初始化角色与库（幂等，可重复执行）：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc 'cd /mnt/d/AI_Coding/kubedeck && bash scripts/dev-db.sh'
```

脚本创建 `kubedeck` 角色与 `kubedeck` / `kubedeck_test` 两个库，把连接串写入 `.testenv/db.env`（已 gitignore，绝不提交）。带着数据库启动服务：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc '
  cd /mnt/d/AI_Coding/kubedeck &&
  set -a && . ./.testenv/db.env && set +a &&
  KUBEDECK_ADDR=0.0.0.0:8080 \
  KUBEDECK_DEV_KUBECONFIG=/mnt/d/AI_Coding/kubedeck/.testenv/k3s.yaml \
  ./bin/kubedeck
'
```

启动时自动执行迁移并补齐审计月分区（未来 3 个月），日志出现 `database ready` 即成功；之后每日巡检会删除超过保留期（默认 180 天）的整月分区。

数据层集成测试（连真实库，每个用例重置 `public` schema，库名必须包含 `kubedeck_test` 才允许执行）：

```bash
wsl.exe -d Ubuntu -u root -- bash -lc '
  cd /mnt/d/AI_Coding/kubedeck && set -a && . ./.testenv/db.env && set +a &&
  /usr/local/go/bin/go test ./internal/store/ -v
'
```

主密钥用于集群凭据加密（design D7），通过 `KUBEDECK_MASTER_KEY`（base64 编码的 32 字节）与 `KUBEDECK_MASTER_KEY_VERSION` 注入；轮换期间用 `KUBEDECK_PREVIOUS_MASTER_KEY` / `KUBEDECK_PREVIOUS_MASTER_KEY_VERSION` 保留旧密钥解密存量密文。生成示例：`openssl rand -base64 32`。主密钥只经环境变量或密钥管理系统注入，不写进仓库，也不入库。

## 8. 常见问题

| 现象 | 根因 | 处理 |
|---|---|---|
| Pod 一直 `ContainerCreating`，事件含 `failed to load flannel 'subnet.env'` | k3s 启动时 flannel 尚未写入 subnet 文件，CNI 先执行了 | 等 `/run/flannel/subnet.env` 出现后重启 k3s；或删除卡住的 Pod 触发重建 |
| 镜像拉取报 `lookup production.cloudfront.docker.com: Try again` | 未配置镜像加速或配置后未重启 | 检查 `registries.yaml` 并 `systemctl restart k3s` |
| Windows 侧 `kubectl` 连 `127.0.0.1:6443` 间歇性 `connection refused` | WSL2 localhost 转发不稳定，且防火墙可能拦截 vEthernet 入站 | 验证与联调统一在 WSL 内执行；不要依赖宿主机端口转发 |
| `wsl: experimental.pageReporting ... 无法识别` | 用户 `.wslconfig` 含未支持的键 | 无害，可忽略；如需消除，删除 `.wslconfig` 中该行 |
| `go: module lookup disabled` 或下载超时 | 未设置 GOPROXY | `export GOPROXY=https://goproxy.cn,direct` |
| `ServerPreferredResources` 报 `stale GroupVersion discovery` | 聚合 API（如 metrics-server）刚启动未就绪 | 属于预期情况，代码必须容忍部分失败（见验证一） |
| WSL 空闲后自动关机，集群不可用 | WSL 的默认省电行为 | `wsl.exe -d Ubuntu -u root -- systemctl start k3s` |

## 9. 停止、清理与重置

```bash
# 只停集群，保留数据
wsl.exe -d Ubuntu -u root -- systemctl stop k3s

# 完全卸载 k3s（会删除 /var/lib/rancher 下的数据）
wsl.exe -d Ubuntu -u root -- /usr/local/bin/k3s-uninstall.sh

# 关闭整个 WSL
wsl.exe --shutdown
```

重置后重新按第 3 节开始即可。

## 10. 环境差异（本地 vs 生产）

| 维度 | 本地（WSL + k3s） | 生产 |
|---|---|---|
| 集群规模 | 单节点、个位数 Pod | 多节点、数千 Pod |
| 存储 | local-path | 云盘 / Ceph |
| 入口 | 无 Ingress | Ingress + TLS + cert-manager |
| IdP | 可选（验证阶段可不接） | 必须接入 OIDC |
| 镜像 | 国内镜像加速 | 私有镜像仓库 |
| 审计 | 本地 PG | 独立 PG + 归档 |
| 高可用 | 单副本 | ≥2 副本 + PDB |
| 网络 | WSL NAT，localhost 转发不稳 | 稳定内网，NetworkPolicy 收敛 |

本地验证只覆盖**能力可行性**，性能与高可用结论必须来自目标规模的压测。