# 备份与恢复

KubeDeck 的持久化状态只有 PostgreSQL：平台用户、集群登记、加密凭据、角色绑定与审计日志。集群资源状态不在数据库中。

## 1. 备份对象

| 对象 | 方式 | 说明 |
|---|---|---|
| PostgreSQL | `pg_dump` / PITR | 平台元数据与审计 |
| 主密钥 | Vault/KMS/离线保管 | 丢失后已登记集群凭据无法解密 |
| Helm values | Git/配置库（不含明文 Secret） | 便于重建 release |
| Secret | 外部 Secret 管理系统 | 数据库密码、OIDC secret、主密钥 |

主密钥与数据库备份必须分开保管：只有数据库备份无法恢复集群凭据；只有主密钥无法恢复元数据。

## 2. 逻辑备份

```bash
pg_dump --format=custom --no-owner --no-privileges \
  --file=kubedeck-$(date +%F).dump \
  'postgres://kubedeck@pg.example.com:5432/kubedeck?sslmode=require'
```

建议每日全量 + 持续 WAL 归档（PITR）。审计表按月分区，备份工具需要支持分区表；`pg_dump` 会包含所有分区。

## 3. 恢复

1. 停止 Kubedeck（缩容到 0），避免恢复期间写入。
2. 在空库恢复：

```bash
createdb -h pg.example.com -U postgres kubedeck_restore
pg_restore --no-owner --no-privileges --dbname=kubedeck_restore kubedeck-2026-11-12.dump
```

3. 确认 `schema_migrations`、`clusters`、`users`、`audit_logs` 行数与备份前一致。
4. 用原主密钥（或轮换后的当前/上一版本密钥）配置 `KUBEDECK_MASTER_KEY`；先不要更换主密钥。
5. 启动一个副本，检查 `/readyz`、登录、集群列表与一条凭据读取。
6. 校验审计哈希链：

```bash
curl -b 'kubedeck_session=<cookie>' \
  'https://kubedeck.example.com/api/v1/admin/audit-logs/verify?cluster=<cluster>'
```

7. 全量切换流量。

## 4. PITR

如果使用 WAL 归档，恢复到故障前时间点：

```bash
# 在 PostgreSQL 恢复配置中设置
restore_command = 'cp /archive/%f %p'
recovery_target_time = '2026-11-12 21:00:00+08'
```

恢复后检查审计链是否完整；PITR 可能截断最后几条异步审计记录，属于可接受的数据丢失窗口（默认刷盘间隔 1 秒）。如果合规要求零丢失，需要同步审计写入或外部不可变日志。

## 5. 主密钥保管

- 生产使用 Vault/KMS，启用审计与双人审批。
- 离线副本放在受控保险柜，至少两份、异地保存。
- 轮换时保留上一版本密钥直到所有凭据重新加密。
- 禁止把主密钥写入 Helm values 文件、CI 变量日志或工单。

## 6. 演练

每季度在隔离环境执行一次：

1. 从最近备份恢复到新库。
2. 用当前主密钥启动 Kubedeck。
3. 登录、浏览一个集群、读取一个 Secret 的元数据（不导出内容）。
4. 校验审计链。
5. 记录恢复耗时（RTO）与丢失窗口（RPO），更新本手册。

2026-11-12 在本地 k3s 测试环境执行过一次快速演练：`pg_dump -Fc` → 新建库 → `pg_restore`，随后比对 `users/clusters/cluster_credentials/role_bindings/audit_logs` 行数，全部一致（1/1/1/0/10）。演练后立即删除恢复库与 dump 文件。