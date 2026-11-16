# 提案：引导式资源创建

## 背景

当前"新建"只给出一个空壳 YAML：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ""
```

用户必须默写 `spec.selector`、`template`、`containers`、`image`、`ports`、`resources`，Service 还要写 `targetPort`，ConfigMap/Secret 要自己处理 base64。dryRun 能防错，但不能降低门槛。数据库里的 `operation_templates` 表也从未被使用。

## 目标

1. 高频类型提供结构化表单：Deployment、StatefulSet、Service、ConfigMap、Secret、CronJob、Ingress、Namespace。
2. 表单与 YAML 双向同步，随时切换；YAML 作为高级模式保留。
3. 内置可运行模板（nginx、redis、定时任务、集群 IP Service），一键填入。
4. Secret 用键值对编辑，自动 base64，不把编码负担丢给用户。
5. 创建前本地校验 + 服务端 dryRun；创建后跳到详情并高亮新对象。
6. 从现有工作负载"复制为新"，预填镜像、端口、环境变量。

## 非目标

- 不覆盖全部 CRD；未知类型仍走 YAML。
- 不实现表单 schema 推断（不做 OpenAPI → 表单生成）。
- 不引入表单库；使用 AntD Form + 受控组件。

## 影响

- `web/src/features/resources`：新建流程重做为页面/Drawer + 表单模型。
- 复用现有 `POST /resources/...`（YAML body）与 dryRun。
- 不改变后端创建契约。