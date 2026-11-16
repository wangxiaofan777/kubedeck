# 设计：引导式资源创建

## 1. 入口

- 列表页「新建」→ 全屏页面 `/clusters/:cluster/new/:typeKey?namespace=`（保留 URL 可分享）。
- 详情页「复制为新」→ 同页面，预填当前对象的关键字段。
- 命令面板「新建 Deployment」→ 同一页面。

页面布局：

```
┌ 页面头：新建 Deployment / 命名空间 prod
├ 左侧 380px：模板选择 + 表单
└ 右侧自适应：YAML 预览（只读，实时生成）｜ 高级模式可编辑
```

## 2. 表单模型

统一 `WorkloadFormValues`：

```ts
{
  name: string
  namespace: string
  image: string
  replicas: number
  containerPort?: number
  servicePort?: number
  env: { key: string; value: string }[]
  cpuRequest?: string
  memoryRequest?: string
  cpuLimit?: string
  memoryLimit?: string
  labels: Record<string, string>
  command?: string[]
}
```

按类型生成 YAML：

- Deployment：`apps/v1` + selector `app=<name>` + template labels + 容器。
- StatefulSet：增加 `serviceName` 与 headless Service 提示。
- Service：ClusterIP/NodePort/LoadBalancer，selector 默认 `app=<name>`，端口映射。
- ConfigMap：键值对 → `data`。
- Secret：键值对 → `stringData`（服务端自动编码），类型默认 `Opaque`。
- CronJob：schedule（cron 表达式）、镜像、命令、重启策略 `OnFailure`。
- Ingress：host、path、pathType、后端 Service 与端口、TLS 开关。
- Namespace：名称 + labels。

生成器为纯函数，便于单测：`buildManifest(kind, values) => object`。

## 3. 双向同步

- 表单 → YAML：每次字段变更 debounce 200ms 重新生成；用户未手动编辑 YAML 时自动覆盖。
- YAML → 表单：切到高级模式后用户编辑 YAML，再切回表单时：
  - 若 YAML 可解析且 kind 与当前类型一致，尽量回填已知字段；
  - 无法回填的字段保留在 YAML 中，切回表单时提示「部分字段仅在 YAML 中可编辑」。
- 不允许 YAML 与表单同时编辑；用 Tabs 明确模式。

## 4. 模板

内置：

| 模板 | 内容 |
|---|---|
| Web 服务 | nginx:1.27，80 端口，2 副本，requests/limits |
| 定时任务 | busybox，每小时执行 `date` |
| Redis | redis:7，6379，1 副本，emptyDir |
| 内部服务 | ClusterIP，80 → 容器 8080 |

模板是 `buildManifest` 的预设值，不是另一套 YAML 字符串，避免漂移。

## 5. 校验与提交

- 本地：名称 DNS label、镜像非空、端口 1–65535、CPU/内存格式、Cron 五段。
- 服务端：`dryRun=All` 创建；失败展示字段级错误（尽可能从 apiserver message 提取）。
- 提交按钮双阶段：`校验` → 成功后启用 `创建`；也允许直接创建（自动先 dryRun）。
- 创建成功：关闭页面，跳到详情，返回列表时保留筛选。

## 6. 可访问性与键盘

- `⌘/Ctrl + Enter` 提交，`Esc` 关闭（有未保存内容时二次确认）。
- 所有输入有 label；错误聚焦到第一个失败字段。

## 7. 测试

- `buildManifest` 对 8 种类型的快照测试。
- Secret 表单不出现 base64 字符串。
- YAML 与表单切换不丢字段。
- 创建失败时保留表单内容。