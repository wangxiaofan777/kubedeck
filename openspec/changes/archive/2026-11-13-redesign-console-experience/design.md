# 设计：控制台信息架构与交互重设计

## 1. 信息架构

```
集群选择（顶栏）
└─ 总览
└─ 工作负载：Pods / Deployments / StatefulSets / DaemonSets / Jobs / CronJobs
└─ 网络：Services / Ingresses
└─ 配置：ConfigMaps / Secrets
└─ 集群：Nodes / Namespaces / Events
└─ 审计（平台管理员）
└─ 集群管理（平台管理员）
```

路由：

| 路径 | 页面 |
|---|---|
| `/` | 重定向到首个集群总览，无集群时进入接入引导 |
| `/clusters` | 集群列表（管理员） |
| `/clusters/new` | 接入集群向导 |
| `/clusters/:cluster/overview` | 总览 |
| `/clusters/:cluster/nodes` | 节点列表 |
| `/clusters/:cluster/nodes/:name` | 节点详情 |
| `/clusters/:cluster/:typeKey` | 资源列表 |
| `/clusters/:cluster/:typeKey/:namespace/:name` | 资源详情工作台 |
| `/clusters/:cluster/audit` | 审计 |
| `/clusters/:cluster/settings` | 集群设置（凭据、访问、危险操作） |

命名空间通过 `?namespace=` 保留，列表页头部提供上下文切换；详情页通过面包屑切换时回到列表。

## 2. 外壳

- 顶栏 56px：品牌、集群切换（带健康点）、命令面板入口（显示 `⌘K`）、用户菜单。
- 侧栏 240px 深色：只放导航；当前项高亮；分组标题小号大写。
- 内容区：`#f5f6f8` 背景，页面容器最大宽度 1600，卡片白底、10px 圆角、1px 边框、极浅阴影。
- 页面头：面包屑（12px 次要色）+ 标题（20px/600）+ 描述/标签 + 右侧操作区。
- 页面切换不整页刷新；列表保留筛选与分页。

## 3. 设计 token

`ConfigProvider.theme.token`：

- `colorPrimary: #2563eb`，`colorInfo` 同色；`colorSuccess: #16a34a`，`colorWarning: #d97706`，`colorError: #dc2626`。
- `borderRadius: 8`，`fontSize: 13`，`fontFamily` 使用系统栈 + `PingFang SC`。
- `components.Table`：`headerBg: #f8fafc`、`headerColor: #475569`、`cellPaddingBlock: 10`、`rowHoverBg: #f8fafc`。
- `components.Card`：`paddingLG: 20`、`boxShadowTertiary` 关闭，改用边框。
- 深色侧栏：`#0f172a`，选中项 `#1e293b` + 左侧 3px 主色条。

## 4. 任务承载

| 任务 | 现在 | 目标 |
|---|---|---|
| 新建资源 | Modal + YAML | 全屏页面 `/new/:typeKey` 或 760px Drawer，表单/YAML 双模式 |
| 编辑 YAML | 详情页 Tab | 保持 Tab，但编辑器全高、可展开为全屏 Drawer |
| 批量删除 | Modal | 选中后底部操作栏 → 确认 Drawer 列出对象 |
| 扩缩容 | Modal | 右侧 Drawer，显示当前/目标副本与影响 |
| 回滚 | Modal | 右侧 Drawer，版本列表 + Diff |
| 日志 | 详情页 Tab | 详情页底部可拖拽面板，或独立路由 `/logs` |
| 全局搜索 | Modal | 命令面板（`⌘K`），搜索 + 动作 |
| 集群接入 | 无 | 三步向导页面 |
| 节点操作 | 无 | 节点详情页 + 确认 Drawer |

Drawer 宽度 480–720，标题包含对象标识；Drawer 内不嵌套 Drawer，危险确认使用 Drawer 内的二次确认区或独立 Modal。

## 5. 详情工作台

- 主区：概览 / 配置 / YAML / 事件 / 日志 / 关联资源。
- 右栏 300px：状态徽标、关键指标、Owner 引用（可点）、标签、操作按钮（危险操作分组）。
- 关联规则：
  - Pod：Owner（ReplicaSet/StatefulSet/DaemonSet/Job）→ 对应详情；Node → 节点详情；Service selector 匹配 → 服务列表筛选。
  - Deployment：Pod 列表按 `spec.selector` 过滤。
  - Node：该节点上的 Pod 列表（`spec.nodeName`）。
- 所有跨资源跳转写入 URL，可分享。

## 6. 总览仪表盘

- 顶部 4 个指标卡：节点就绪、Pod 运行、异常工作负载、命名空间；每张卡可点击下钻到对应列表。
- 用量卡：CPU/内存当前值 + 会话趋势（SVG 面积图）+ 可分配基线。
- Pod 状态分布：水平堆叠条 + 图例。
- 工作负载健康：三类工作负载的总数/不可用数 + 迷你条形。
- 最近事件：只显示 Warning 前 8 条，点击跳到事件列表。
- 节点健康：就绪/总数 + 异常节点名（若有）。

## 7. 命令面板

- 全局快捷键 `Ctrl/⌘ + K`；输入 ≥2 字符后 300ms 防抖。
- 结果分组：资源（5 类核心资源，最多各 10 条）、导航（总览/节点/审计/设置）、动作（新建 Deployment、接入集群）。
- 支持上下键选择、Enter 打开、Esc 关闭；无结果与请求失败使用不同文案。

## 8. 样式与状态

- 统一 `PageHeader`、`StatCard`、`EmptyState`、`ErrorState`、`StatusPill` 组件。
- 表格：默认 13px，表头 12px 大写；行高 40；状态用 pill 而非裸 Tag。
- 加载：首屏骨架屏，刷新时只显示顶部进度条，不遮罩表格。
- 错误：展示错误码、requestId 与重试；403/404 使用整页 Result。
- 日期：统一 `YYYY-MM-DD HH:mm`，相对时间用于 Age。

## 9. 性能

- `lib/monaco.ts` 改为动态 `import()`，`YamlCodeEditor` 内部懒加载；`ResourceCreateDrawer` 也懒加载。
- 主包目标 < 1.2 MB。
- 列表 columns 使用 `useMemo`，表格组件 `memo`。
- 日志使用环形缓冲（最多 20,000 行 / 5 MB）与按帧批量 flush。
- 轮询在页面不可见时暂停。