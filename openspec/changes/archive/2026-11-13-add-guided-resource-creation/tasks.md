## 1. 表单模型

- [x] 1.1 定义各类型的表单值模型与默认值
- [x] 1.2 实现 `buildManifest(kind, values)` 纯函数并覆盖 8 种类型
- [x] 1.3 实现模板预设（Web 服务、Redis、定时任务、内部服务）
- [x] 1.4 从现有对象提取表单值（复制为新）

## 2. 创建页面

- [x] 2.1 新增 `/clusters/:cluster/new/:typeKey` 页面（表单 + YAML 预览）
- [x] 2.2 表单字段与类型切换
- [x] 2.3 表单/YAML 双向同步与冲突提示
- [x] 2.4 本地校验 + 服务端 dryRun + 提交
- [x] 2.5 Secret 键值对编辑（stringData）
- [x] 2.6 创建成功跳转详情、失败保留输入

## 3. 集成

- [x] 3.1 列表页与详情页「复制为新」入口
- [x] 3.2 命令面板「新建」入口
- [x] 3.3 未知类型回退 YAML 模式

## 4. 验证

- [x] 4.1 `buildManifest` 快照测试
- [x] 4.2 前端 lint/typecheck/build 通过
- [x] 4.3 在 k3s 上创建 Deployment/Service/Secret 并验证可运行